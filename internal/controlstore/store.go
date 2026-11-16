// Package controlstore persists desired/applied configuration and recoverable
// work. The HTTP server is only a transport; tasks outlive individual requests.
package controlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	ID      int64           `json:"id"`
	Kind    string          `json:"kind"`
	Actor   string          `json:"actor"`
	State   string          `json:"state"`
	Phase   string          `json:"phase"`
	Error   string          `json:"error,omitempty"`
	Created int64           `json:"created"`
	Updated int64           `json:"updated"`
	Input   json.RawMessage `json:"-"`
	Result  json.RawMessage `json:"result,omitempty"`
}

func Open(path string) (*Store, error) {
	db, err := database.Open(path)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS control_state(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL,applied_revision INTEGER NOT NULL,desired BLOB NOT NULL,applied BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS control_tasks(id INTEGER PRIMARY KEY AUTOINCREMENT,kind TEXT NOT NULL,actor TEXT NOT NULL,state TEXT NOT NULL,phase TEXT NOT NULL,error TEXT NOT NULL DEFAULT '',created INTEGER NOT NULL,updated INTEGER NOT NULL,input BLOB NOT NULL,result BLOB NOT NULL DEFAULT '{}');
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO control_state(id,revision,applied_revision,desired,applied) VALUES(1,1,1,?,?)`, data, data)
	return err
}
func (s *Store) State(ctx context.Context) (v State, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT revision,applied_revision,desired,applied FROM control_state WHERE id=1`).Scan(&v.Revision, &v.AppliedRevision, &v.Desired, &v.Applied)
	return
}
func (s *Store) Queue(ctx context.Context, kind, actor string, input []byte, expected int64, desired []byte) (int64, error) {
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
		if pending > 0 {
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
	r, err := tx.ExecContext(ctx, `INSERT INTO control_tasks(kind,actor,state,phase,created,updated,input) VALUES(?,?,'queued','queued',?,?,?)`, kind, actor, now, now, input)
	if err != nil {
		return 0, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
func (s *Store) Claim(ctx context.Context) (v Task, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE control_tasks SET updated=updated WHERE state='queued'`); err != nil {
		return v, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id,kind,actor,state,phase,error,created,updated,input FROM control_tasks WHERE state='queued' ORDER BY id LIMIT 1`).Scan(&v.ID, &v.Kind, &v.Actor, &v.State, &v.Phase, &v.Error, &v.Created, &v.Updated, &v.Input)
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
	_, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET result=? WHERE id=?`, result, id)
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
	_, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET state=?,phase=CASE WHEN ?='failed' THEN phase ELSE ? END,error=?,updated=?,input='{}' WHERE id=?`, state, state, state, detail, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM control_tasks WHERE state IN ('succeeded','failed','cancelled') AND id<(SELECT COALESCE(MAX(id),0)-200 FROM control_tasks)`)
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

// Only an interrupted configuration application is replayed automatically.
// Non-idempotent operations require an explicit new request after interruption.
func (s *Store) Recover(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET state=CASE WHEN kind='apply' THEN 'queued' ELSE 'failed' END,input=CASE WHEN kind='apply' THEN input ELSE CAST('{}' AS BLOB) END,phase='interrupted',error='process interrupted; inspect state before retrying',updated=? WHERE state='running'`, time.Now().Unix())
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
	err = s.db.QueryRowContext(ctx, `SELECT id,kind,actor,state,phase,error,created,updated,CAST(result AS BLOB) FROM control_tasks WHERE id=?`, id).Scan(&t.ID, &t.Kind, &t.Actor, &t.State, &t.Phase, &t.Error, &t.Created, &t.Updated, &t.Result)
	return
}
func (s *Store) tasks(ctx context.Context, result string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,actor,state,phase,error,created,updated,`+result+` FROM control_tasks ORDER BY id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	v := []Task{}
	for rows.Next() {
		var t Task
		if err = rows.Scan(&t.ID, &t.Kind, &t.Actor, &t.State, &t.Phase, &t.Error, &t.Created, &t.Updated, &t.Result); err != nil {
			return nil, err
		}
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
