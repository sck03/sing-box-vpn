package controlstore

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskRetentionBudgetsAndPending(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().Unix()
	_, err = s.db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<220)
 INSERT INTO control_tasks(kind,actor,state,phase,created,updated,input,result) SELECT 'test','test','succeeded','succeeded',?,?,'{}',? FROM n`, now, now, `{"outcome":"partial","data":"`+strings.Repeat("测", 85<<10)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT INTO control_tasks(kind,actor,state,phase,created,updated,input) VALUES('apply','test','queued','queued',0,0,'{}')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PruneHistory(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	var count, size, pending int
	if err = s.db.QueryRow(`SELECT COUNT(*),SUM(length(CAST(result AS BLOB))) FROM control_tasks WHERE state='succeeded'`).Scan(&count, &size); err != nil {
		t.Fatal(err)
	}
	if count != 200 || size > 8<<20 {
		t.Fatal(count, size)
	}
	var outcomes int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM control_tasks WHERE json_extract(result,'$.outcome')='partial'`).Scan(&outcomes); err != nil || outcomes != count {
		t.Fatal("retention discarded outcome", outcomes, err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM control_tasks WHERE state='queued'`).Scan(&pending); err != nil || pending != 1 {
		t.Fatal(pending, err)
	}
}
