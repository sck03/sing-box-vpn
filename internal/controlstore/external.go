package controlstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// CompleteExternal consumes only a matching, still-running deployment. Result,
// final state and phase commit together; replay cannot overwrite a later task.
func (s *Store) CompleteExternal(ctx context.Context, id int64, phase, target string, result []byte, detail string) (bool, error) {
	if len(result) > 8192 || !json.Valid(result) || len(detail) > 2048 {
		return false, errors.New("invalid external task result")
	}
	state, outcome := "succeeded", "passed"
	if detail != "" {
		state, outcome = "failed", "failed"
	}
	r, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET state=?,phase=?,error=?,result=?,outcome=?,updated=?,input='{}'
 WHERE id=? AND kind='program-upgrade' AND state='running' AND phase='awaiting-panel-health' AND json_extract(result,'$.target_version')=?`, state, phase, detail, result, outcome, time.Now().Unix(), id, target)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

func (s *Store) ExpireExternal(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET state='failed',phase='interrupted',error='未收到升级健康检查结果，请检查服务状态和升级日志',outcome='failed',result=json_set(result,'$.outcome','failed','$.phase','interrupted'),input='{}',updated=?
 WHERE kind='program-upgrade' AND state='running' AND phase='awaiting-panel-health' AND updated<?`, time.Now().Unix(), before.Unix())
	return err
}
