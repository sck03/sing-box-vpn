// Package controlstore persists desired/applied configuration and recoverable
// work. The HTTP server is only a transport; tasks outlive individual requests.
package controlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/sck03/sing-box-vpn/internal/database"
	"github.com/sck03/sing-box-vpn/internal/domain"
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
	ID              int64               `json:"id"`
	Kind            string              `json:"kind"`
	Actor           string              `json:"actor"`
	State           string              `json:"state"`
	Phase           string              `json:"phase"`
	Error           string              `json:"error,omitempty"`
	Created         int64               `json:"created"`
	Updated         int64               `json:"updated"`
	Result          json.RawMessage     `json:"result,omitempty"`
	Outcome         string              `json:"outcome,omitempty"`
	Acknowledged    bool                `json:"acknowledged"`
	Details         domain.TaskAudience `json:"details"`
	Cancellable     bool                `json:"cancellable"`
	CancelRequested bool                `json:"cancel_requested"`
}

// ClaimedTask is execution data, available only after claiming queued work.
// Public task details cannot accidentally be passed to a worker without Input.
type ClaimedTask struct {
	Task
	Input json.RawMessage `json:"-"`
}

func Open(path string) (*Store, error) {
	db, err := database.Open(path)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS control_state(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL,applied_revision INTEGER NOT NULL,desired BLOB NOT NULL,applied BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS control_tasks(id INTEGER PRIMARY KEY AUTOINCREMENT,kind TEXT NOT NULL,actor TEXT NOT NULL,source TEXT NOT NULL DEFAULT 'user' CHECK(source IN ('user','cli','scheduler')),lane TEXT NOT NULL DEFAULT 'control' CHECK(lane IN ('control','subscription','probe')),state TEXT NOT NULL,phase TEXT NOT NULL,error TEXT NOT NULL DEFAULT '',created INTEGER NOT NULL,updated INTEGER NOT NULL,input BLOB NOT NULL,result BLOB NOT NULL DEFAULT '{}',outcome TEXT NOT NULL DEFAULT '',acknowledged INTEGER NOT NULL DEFAULT 0 CHECK(acknowledged IN (0,1)),cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK(cancel_requested IN (0,1)));
CREATE INDEX IF NOT EXISTS control_task_lane ON control_tasks(state,lane,id);
CREATE TABLE IF NOT EXISTS task_requests(actor TEXT NOT NULL,request_id TEXT NOT NULL,fingerprint TEXT NOT NULL,task_id INTEGER NOT NULL,created INTEGER NOT NULL,PRIMARY KEY(actor,request_id));
CREATE INDEX IF NOT EXISTS task_request_created ON task_requests(created);
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

type AppliedState struct {
	Revision int64
	Config   json.RawMessage
}

// Configuration and its confirmation generation must be read atomically.
func (s *Store) AppliedState(ctx context.Context) (v AppliedState, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT applied_revision,applied FROM control_state WHERE id=1`).Scan(&v.Revision, &v.Config)
	return
}

// AppliedStateSince avoids copying the configuration when its confirmed revision
// is unchanged. Both values come from one database snapshot.
func (s *Store) AppliedStateSince(ctx context.Context, revision int64) (v AppliedState, err error) {
	var data []byte
	err = s.db.QueryRowContext(ctx, `SELECT applied_revision,CASE WHEN applied_revision != ? THEN applied ELSE NULL END FROM control_state WHERE id=1`, revision).Scan(&v.Revision, &data)
	v.Config = data
	return
}

// AppliedView includes pending-state metadata in the same SQLite snapshot.
// A revision hit returns no configuration body, including when only desired changed.
type AppliedView struct {
	Revisions
	Config json.RawMessage
}

func (s *Store) AppliedViewSince(ctx context.Context, since int64) (v AppliedView, err error) {
	var data []byte
	err = s.db.QueryRowContext(ctx, `SELECT revision,applied_revision,CASE WHEN applied_revision != ? THEN applied ELSE NULL END FROM control_state WHERE id=1`, since).Scan(&v.Revision, &v.AppliedRevision, &data)
	v.Config = data
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
func (s *Store) ClaimLane(ctx context.Context, lane string) (v ClaimedTask, err error) {
	if !validLane(lane) {
		return v, errors.New("unknown task lane")
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
	err = tx.QueryRowContext(ctx, `SELECT id,kind,actor,state,phase,error,created,updated,input FROM control_tasks WHERE state='queued' AND lane=? AND NOT EXISTS (SELECT 1 FROM control_tasks WHERE state='running' AND lane=?) ORDER BY id LIMIT 1`, lane, lane).Scan(&v.ID, &v.Kind, &v.Actor, &v.State, &v.Phase, &v.Error, &v.Created, &v.Updated, &v.Input)
	if err != nil {
		return v, err
	}
	v.Updated = time.Now().Unix()
	_, err = tx.ExecContext(ctx, `UPDATE control_tasks SET state='running',phase='validating',updated=? WHERE id=?`, v.Updated, v.ID)
	if err != nil {
		return v, err
	}
	if err = tx.Commit(); err != nil {
		return v, err
	}
	v.State, v.Phase = "running", "validating"
	return v, nil
}
func (s *Store) Phase(ctx context.Context, id int64, phase string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET phase=?,updated=? WHERE id=? AND cancel_requested=0`, phase, time.Now().Unix(), id)
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
	_, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET result=?,outcome=? WHERE id=? AND cancel_requested=0`, result, summary.Outcome, id)
	return err
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

func (s *Store) RecentTaskSummaries(ctx context.Context) ([]Task, error) {
	return s.queryTasks(ctx, `SELECT id,kind,actor,state,phase,error,created,updated,CAST('{}' AS BLOB),outcome,acknowledged,`+taskCancellationColumns+` FROM control_tasks ORDER BY state IN ('queued','running') DESC,id DESC LIMIT 12`)
}
func (s *Store) Task(ctx context.Context, id int64) (t Task, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT id,kind,actor,state,phase,error,created,updated,CAST(result AS BLOB),outcome,acknowledged,`+taskCancellationColumns+` FROM control_tasks WHERE id=?`, id).Scan(&t.ID, &t.Kind, &t.Actor, &t.State, &t.Phase, &t.Error, &t.Created, &t.Updated, &t.Result, &t.Outcome, &t.Acknowledged, &t.CancelRequested, &t.Cancellable)
	t.Details = domain.TaskDetails(t.Kind, t.Actor)
	return
}

func (s *Store) queryTasks(ctx context.Context, query string, args ...any) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	v := []Task{}
	for rows.Next() {
		var t Task
		if err = rows.Scan(&t.ID, &t.Kind, &t.Actor, &t.State, &t.Phase, &t.Error, &t.Created, &t.Updated, &t.Result, &t.Outcome, &t.Acknowledged, &t.CancelRequested, &t.Cancellable); err != nil {
			return nil, err
		}
		t.Details = domain.TaskDetails(t.Kind, t.Actor)
		v = append(v, t)
	}
	return v, rows.Err()
}
func (s *Store) Heartbeat(ctx context.Context, instance, version string, revision, started int64) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `INSERT INTO runtime_state(instance,revision,version,started,heartbeat) VALUES(?,?,?,?,?) ON CONFLICT(instance) DO UPDATE SET revision=excluded.revision,version=excluded.version,started=excluded.started,heartbeat=excluded.heartbeat`, instance, revision, version, started, now)
	return err
}

type RuntimeState struct {
	Instance  string `json:"instance"`
	Version   string `json:"version"`
	Revision  int64  `json:"revision"`
	Started   int64  `json:"started"`
	Heartbeat int64  `json:"heartbeat,omitempty"`
	Recent    bool   `json:"recent"`
}

func (s *Store) Runtimes(ctx context.Context) ([]RuntimeState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT instance,revision,version,started,heartbeat FROM runtime_state ORDER BY instance`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RuntimeState{}
	now := time.Now().Unix()
	for rows.Next() {
		var state RuntimeState
		if err = rows.Scan(&state.Instance, &state.Revision, &state.Version, &state.Started, &state.Heartbeat); err != nil {
			return nil, err
		}
		state.Recent = now-state.Heartbeat < 10
		out = append(out, state)
	}
	return out, rows.Err()
}
