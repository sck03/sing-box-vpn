// Package controlstore persists desired/applied configuration and recoverable
// work. The HTTP server is only a transport; tasks outlive individual requests.
package controlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/database"
	"time"
)

var ErrConflict = errors.New("configuration revision conflict; reload before saving")

type Store struct{ db *sql.DB }
type State struct {
	Revision        int64           `json:"revision"`
	AppliedRevision int64           `json:"applied_revision"`
	Desired         json.RawMessage `json:"desired"`
	Applied         json.RawMessage `json:"applied"`
}
type Task struct {
	ID           int64                    `json:"id"`
	Kind         string                   `json:"kind"`
	Actor        string                   `json:"actor"`
	State        string                   `json:"state"`
	Phase        string                   `json:"phase"`
	Error        string                   `json:"error,omitempty"`
	Created      int64                    `json:"created"`
	Updated      int64                    `json:"updated"`
	Input        json.RawMessage          `json:"-"`
	Result       json.RawMessage          `json:"result,omitempty"`
	Outcome      string                   `json:"outcome,omitempty"`
	Acknowledged bool                     `json:"acknowledged"`
	Details      application.TaskAudience `json:"details"`
}

func Open(path string) (*Store, error) {
	db, err := database.Open(path)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS control_state(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL,applied_revision INTEGER NOT NULL,desired BLOB NOT NULL,applied BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS control_tasks(id INTEGER PRIMARY KEY AUTOINCREMENT,kind TEXT NOT NULL,actor TEXT NOT NULL,lane TEXT NOT NULL DEFAULT 'control' CHECK(lane IN ('control','background')),state TEXT NOT NULL,phase TEXT NOT NULL,error TEXT NOT NULL DEFAULT '',created INTEGER NOT NULL,updated INTEGER NOT NULL,input BLOB NOT NULL,result BLOB NOT NULL DEFAULT '{}',outcome TEXT NOT NULL DEFAULT '',acknowledged INTEGER NOT NULL DEFAULT 0 CHECK(acknowledged IN (0,1)));
CREATE INDEX IF NOT EXISTS control_task_lane ON control_tasks(state,lane,id);
CREATE TABLE IF NOT EXISTS runtime_state(instance TEXT PRIMARY KEY,revision INTEGER NOT NULL,version TEXT NOT NULL,started INTEGER NOT NULL,heartbeat INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS observations(name TEXT PRIMARY KEY,value TEXT NOT NULL);
`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Observation(ctx context.Context, name string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM observations WHERE name=?`, name).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return value, err
}
func (s *Store) Observe(ctx context.Context, name, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO observations(name,value) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET value=excluded.value`, name, value)
	return err
}
func (s *Store) Initialize(ctx context.Context, data []byte) error {
	return s.InitializeWith(ctx, data, nil)
}

// InitializeWith commits configuration and dependent initial records together.
func (s *Store) InitializeWith(ctx context.Context, data []byte, initialize func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO control_state(id,revision,applied_revision,desired,applied) VALUES(1,1,1,?,?)`, data, data); err != nil {
		return err
	}
	if initialize != nil {
		if err = initialize(tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type Revisions struct {
	Revision        int64 `json:"revision"`
	AppliedRevision int64 `json:"applied_revision"`
}

func (s *Store) Revisions(ctx context.Context) (v Revisions, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT revision,applied_revision FROM control_state WHERE id=1`).Scan(&v.Revision, &v.AppliedRevision)
	return
}
func (s *Store) AppliedConfig(ctx context.Context) (data json.RawMessage, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT applied FROM control_state WHERE id=1`).Scan(&data)
	return
}

// DesiredState reads the editable configuration and its revisions atomically,
// without copying the potentially large applied configuration into memory.
type DesiredState struct {
	Revisions
	Config json.RawMessage
}

func (s *Store) DesiredState(ctx context.Context) (v DesiredState, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT revision,applied_revision,desired FROM control_state WHERE id=1`).Scan(&v.Revision, &v.AppliedRevision, &v.Config)
	return
}

func (s *Store) State(ctx context.Context) (v State, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT revision,applied_revision,desired,applied FROM control_state WHERE id=1`).Scan(&v.Revision, &v.AppliedRevision, &v.Desired, &v.Applied)
	return
}
func (s *Store) Queue(ctx context.Context, kind, actor string, input []byte, expected int64, desired []byte) (int64, error) {
	return s.queue(ctx, "control", kind, actor, input, expected, desired)
}

func (s *Store) QueueBackground(ctx context.Context, kind, actor string, input []byte) (int64, error) {
	return s.queue(ctx, "background", kind, actor, input, 0, nil)
}

func (s *Store) queue(ctx context.Context, lane, kind, actor string, input []byte, expected int64, desired []byte) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE control_state SET revision=revision WHERE id=1`); err != nil {
		return 0, err
	}
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM control_tasks WHERE state IN ('queued','running')`).Scan(&pending); err != nil {
		return 0, err
	}
	if pending >= 32 {
		return 0, errors.New("task queue is full")
	}
	if desired != nil {
		// Serialize desired configuration changes with active side effects.
		var blocking int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM control_tasks WHERE state IN ('queued','running') AND lane='control'`).Scan(&blocking); err != nil {
			return 0, err
		}
		if blocking > 0 {
			return 0, errors.New("wait for the current task before saving configuration")
		}
		r, e := tx.ExecContext(ctx, `UPDATE control_state SET desired=?,revision=revision+1 WHERE id=1 AND revision=?`, desired, expected)
		if e != nil {
			return 0, e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return 0, e
		}
		if n != 1 {
			return 0, ErrConflict
		}
	}
	now := time.Now().Unix()
	if actor == "scheduler" {
		var duplicate int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM control_tasks WHERE actor=? AND kind=? AND input=? AND state IN ('queued','running')`, actor, kind, input).Scan(&duplicate); err != nil {
			return 0, err
		}
		if duplicate > 0 {
			return 0, tx.Commit()
		}
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO control_tasks(kind,actor,lane,state,phase,created,updated,input) VALUES(?,?,?,'queued','queued',?,?,?)`, kind, actor, lane, now, now, input)
	if err != nil {
		return 0, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
func (s *Store) ClaimLane(ctx context.Context, lane string) (v Task, err error) {
	if lane != "control" && lane != "background" {
		return Task{}, errors.New("unknown task lane")
	}
	// Idle workers must not contend with durable data-plane reservations.
	var queued int
	if err = s.db.QueryRowContext(ctx, `SELECT 1 FROM control_tasks WHERE state='queued' AND lane=? AND NOT EXISTS (SELECT 1 FROM control_tasks WHERE state='running' AND lane=?) LIMIT 1`, lane, lane).Scan(&queued); err != nil {
		return v, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE control_tasks SET updated=updated WHERE id=-1`); err != nil {
		return v, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id,kind,actor,state,phase,error,created,updated,input FROM control_tasks WHERE state='queued' AND lane=? ORDER BY id LIMIT 1`, lane).Scan(&v.ID, &v.Kind, &v.Actor, &v.State, &v.Phase, &v.Error, &v.Created, &v.Updated, &v.Input)
	if err != nil {
		return v, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE control_tasks SET state='running',phase='validating',updated=? WHERE id=?`, time.Now().Unix(), v.ID)
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}
func (s *Store) Phase(ctx context.Context, id int64, phase string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET phase=?,updated=? WHERE id=?`, phase, time.Now().Unix(), id)
	return err
}
func (s *Store) Result(ctx context.Context, id int64, result []byte) error {
	if len(result) > 256<<10 || !json.Valid(result) {
		return errors.New("task result exceeds budget or is not JSON")
	}
	var summary struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(result, &summary); err != nil {
		return err
	}
	if len(summary.Outcome) > 64 {
		return errors.New("task outcome exceeds budget")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET result=?,outcome=? WHERE id=?`, result, summary.Outcome, id)
	return err
}
func (s *Store) Finish(ctx context.Context, id int64, result error) error {
	state, detail := "succeeded", ""
	if result != nil {
		state, detail = "failed", result.Error()
		if len(detail) > 2048 {
			detail = detail[:2048]
		}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET state=?,phase=CASE WHEN ?='failed' THEN phase ELSE ? END,error=?,updated=?,input='{}',outcome=CASE WHEN ?='failed' AND kind='apply' THEN 'failed' ELSE outcome END,result=CASE WHEN ?='failed' AND kind='apply' THEN json_set(result,'$.outcome','failed') ELSE result END WHERE id=?`, state, state, state, detail, time.Now().Unix(), state, state, id)
	if err != nil {
		return err
	}
	return nil
}
func (s *Store) Applied(ctx context.Context, revision int64) error {
	r, err := s.db.ExecContext(ctx, `UPDATE control_state SET applied=desired,applied_revision=revision WHERE id=1 AND revision=?`, revision)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return ErrConflict
	}
	return err
}

// Only an interrupted configuration application is replayed automatically.
// Non-idempotent operations require an explicit new request after interruption.
func (s *Store) Recover(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET state=CASE WHEN kind='apply' THEN 'queued' ELSE 'failed' END,input=CASE WHEN kind='apply' THEN input ELSE CAST('{}' AS BLOB) END,phase='interrupted',error='process interrupted; inspect state before retrying',updated=? WHERE state='running' AND NOT (kind IN ('program-upgrade','panel-restart') AND phase='awaiting-panel-health')`, time.Now().Unix())
	return err
}
func (s *Store) Cancel(ctx context.Context, id int64) error {
	r, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET state='cancelled',phase='cancelled',input='{}',updated=? WHERE id=? AND state='queued' AND kind!='apply'`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("only queued non-configuration tasks can be cancelled")
	}
	return err
}
func (s *Store) Tasks(ctx context.Context) ([]Task, error) {
	return s.tasks(ctx, "CAST(result AS BLOB)")
}
func (s *Store) TaskSummaries(ctx context.Context) ([]Task, error) {
	return s.tasks(ctx, "CAST('{}' AS BLOB)")
}
func (s *Store) Task(ctx context.Context, id int64) (t Task, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT id,kind,actor,state,phase,error,created,updated,CAST(result AS BLOB),outcome,acknowledged FROM control_tasks WHERE id=?`, id).Scan(&t.ID, &t.Kind, &t.Actor, &t.State, &t.Phase, &t.Error, &t.Created, &t.Updated, &t.Result, &t.Outcome, &t.Acknowledged)
	t.Details = application.TaskDetails(t.Kind, t.Actor)
	return
}
func (s *Store) tasks(ctx context.Context, result string) ([]Task, error) {
	policy, err := s.HistoryPolicy(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,actor,state,phase,error,created,updated,`+result+`,outcome,acknowledged FROM control_tasks ORDER BY state IN ('queued','running') DESC,id DESC LIMIT ?`, policy.Count+32)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	v := []Task{}
	for rows.Next() {
		var t Task
		if err = rows.Scan(&t.ID, &t.Kind, &t.Actor, &t.State, &t.Phase, &t.Error, &t.Created, &t.Updated, &t.Result, &t.Outcome, &t.Acknowledged); err != nil {
			return nil, err
		}
		t.Details = application.TaskDetails(t.Kind, t.Actor)
		v = append(v, t)
	}
	return v, rows.Err()
}
func (s *Store) Heartbeat(ctx context.Context, instance, version string, revision, started int64) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `INSERT INTO runtime_state(instance,revision,version,started,heartbeat) VALUES(?,?,?,?,?) ON CONFLICT(instance) DO UPDATE SET revision=excluded.revision,version=excluded.version,started=excluded.started,heartbeat=excluded.heartbeat`, instance, revision, version, started, now)
	return err
}
func (s *Store) Runtimes(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT instance,revision,version,started,heartbeat FROM runtime_state ORDER BY instance`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, v string
		var r, start, h int64
		if err = rows.Scan(&id, &r, &v, &start, &h); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"instance": id, "version": v, "revision": r, "started": start, "heartbeat": h, "recent": time.Now().Unix()-h < 10})
	}
	return out, rows.Err()
}
