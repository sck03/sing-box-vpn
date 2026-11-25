package controlstore

import "context"

type TaskPage struct {
	Items      []Task `json:"items"`
	NextBefore int64  `json:"next_before"`
}

type TaskCounts struct {
	Failed    int `json:"failed"`
	Active    int `json:"active"`
	Attention int `json:"attention"`
}

func (s *Store) TaskCounts(ctx context.Context) (v TaskCounts, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(state='failed' AND acknowledged=0),0),COALESCE(SUM(state IN ('queued','running')),0),COALESCE(SUM(acknowledged=0 AND (state='failed' OR outcome IN ('partial','cached','failed'))),0) FROM control_tasks`).Scan(&v.Failed, &v.Active, &v.Attention)
	return
}

func (s *Store) TaskPage(ctx context.Context, before int64, limit int) (TaskPage, error) {
	limit = max(1, min(limit, 100))
	items, err := s.queryTasks(ctx, `SELECT id,kind,actor,state,phase,'',created,updated,CAST('{}' AS BLOB),outcome,acknowledged,`+taskCancellationColumns+` FROM control_tasks WHERE (?=0 OR id<?) ORDER BY id DESC LIMIT ?`, before, before, limit+1)
	page := TaskPage{Items: items}
	if len(items) > limit {
		page.Items, page.NextBefore = items[:limit], items[limit-1].ID
	}
	return page, err
}
