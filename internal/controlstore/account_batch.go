package controlstore

import (
	"context"
	"errors"
	"time"
)

// ClaimAccountFollowers extends one claimed task with at most 15 consecutive
// account commands. A different control task is an ordering barrier.
func (s *Store) ClaimAccountFollowers(ctx context.Context, lead int64) ([]Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE control_tasks SET updated=updated WHERE id=-1`); err != nil {
		return nil, err
	}
	var valid int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM control_tasks WHERE id=? AND state='running' AND kind='account' AND lane='control' AND NOT EXISTS (SELECT 1 FROM control_tasks WHERE lane='control' AND state='running' AND id!=?)`, lead, lead).Scan(&valid); err != nil {
		return nil, err
	}
	if valid != 1 {
		return nil, errors.New("account batch requires the sole running control task")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,actor,state,phase,error,created,updated,input FROM control_tasks WHERE state='queued' AND lane='control' ORDER BY id LIMIT 15`)
	if err != nil {
		return nil, err
	}
	var tasks []Task
	for rows.Next() {
		var task Task
		if err = rows.Scan(&task.ID, &task.Kind, &task.Actor, &task.State, &task.Phase, &task.Error, &task.Created, &task.Updated, &task.Input); err != nil {
			rows.Close()
			return nil, err
		}
		if task.Kind != "account" {
			break
		}
		tasks = append(tasks, task)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		if _, err = tx.ExecContext(ctx, `UPDATE control_tasks SET state='running',phase='validating',updated=? WHERE id=? AND state='queued'`, time.Now().Unix(), task.ID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return tasks, nil
}
