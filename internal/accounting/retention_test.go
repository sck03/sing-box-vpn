package accounting

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestGrantLifecycleCapacityAndPaging(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	// Previously the lifetime cap included all 10,000 revoked records.
	_, err = s.db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<10000)
 INSERT INTO download_grants(id,scope,profiles,outputs,hash,revoked) SELECT 'old-'||i,'','[]','["out"]','hash-'||i,1 FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.CreateGrant(ctx, Grant{ID: "fresh", Outputs: []string{"out"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.LookupGrant(ctx, token); err != nil {
		t.Fatal(err)
	}
	page, err := s.GrantPage(ctx, "已撤销", 0, 20)
	if err != nil || page.Total != 1000 || len(page.Items) != 20 {
		t.Fatal(page.Total, len(page.Items), err)
	}
	page, err = s.GrantPage(ctx, "fresh", 0, 20)
	if err != nil || page.Total != 1 || page.Items[0].State != "active" {
		t.Fatal(page, err)
	}
	zero := int64(0)
	past := time.Now().Add(-time.Hour)
	for i, g := range []Grant{{MaxDownloads: &zero}, {ExpiresAt: &past}, {Revoked: true}} {
		if g.Status(time.Now()) == "active" {
			t.Fatal("invalid grant shown active", i)
		}
	}
	if err = s.RevokeGrant(ctx, "fresh"); err != nil {
		t.Fatal(err)
	}
	if err = s.PruneHistory(ctx, time.Now().Add(31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.LookupGrant(ctx, token); err == nil {
		t.Fatal("purge resurrected a bearer token")
	}
}

func BenchmarkGrantPage(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "control.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	for i := range 100 {
		if _, err = s.CreateGrant(b.Context(), Grant{ID: fmt.Sprintf("g%d", i), Outputs: []string{"out"}}); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if _, err = s.GrantPage(b.Context(), "out", 0, 20); err != nil {
			b.Fatal(err)
		}
	}
}

func TestHistoryRetentionKeepsUnsettledPeriods(t *testing.T) {
	s, _ := fixture(t, 1<<20)
	_, err := s.db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<130)
	 INSERT INTO usage_periods(account,period,charged,upload,download) SELECT 'alice',i,100,0,0 FROM n;
	 INSERT INTO leases(id,account,runtime,reserved,period) VALUES('unsettled','alice','normal',100,1);`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PruneHistory(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	var count, old int
	if err = s.db.QueryRow(`SELECT COUNT(*),SUM(CASE WHEN period=1 THEN 1 ELSE 0 END) FROM usage_periods`).Scan(&count, &old); err != nil || count != 121 || old != 1 {
		t.Fatal(count, old, err)
	}
	if _, err = s.db.Exec(`UPDATE leases SET closed=1 WHERE id='unsettled'`); err != nil {
		t.Fatal(err)
	}
	if err = s.PruneHistory(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM usage_periods`).Scan(&count); err != nil || count != 120 {
		t.Fatal(count, err)
	}
}
