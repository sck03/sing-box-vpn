package accounting

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

// These disk-backed benchmarks also run unchanged on the pre-optimization
// revision. Keep setup outside timing and do not weaken SQLite durability.
func benchmarkMeter(b *testing.B, accounts int) (*Store, *Meter, []*Handle) {
	b.Helper()
	s, err := Open(filepath.Join(b.TempDir(), "control.db"))
	if err != nil {
		b.Fatal(err)
	}
	m := NewMeter(s, "normal")
	b.Cleanup(func() {
		m.Shutdown()
		if err := m.Close(context.Background()); err != nil {
			b.Error(err)
		}
		s.Close()
	})
	handles := make([]*Handle, 0, accounts)
	for i := range accounts {
		id := fmt.Sprintf("account-%d", i)
		if err = s.SaveAccount(b.Context(), domain.Account{ID: id, Name: id}); err != nil {
			b.Fatal(err)
		}
		if err = s.Bind(b.Context(), domain.Credential{ID: id, AccountID: id, RuntimeID: "normal", InboundID: "in", Protocol: "vless", Generation: 1, UUID: fmt.Sprintf("aabbccdd-1122-4333-8444-%012d", i)}); err != nil {
			b.Fatal(err)
		}
		h, e := m.Open(b.Context(), id+".1", "in", &closedCounter{})
		if e != nil {
			b.Fatal(e)
		}
		handles = append(handles, h)
	}
	return s, m, handles
}

func BenchmarkActiveAuthorization100(b *testing.B) {
	_, m, _ := benchmarkMeter(b, 100)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := m.Tick(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDurableMeter64KiB(b *testing.B) {
	_, _, handles := benchmarkMeter(b, 1)
	h := handles[0]
	b.SetBytes(ReservationBytes)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		n, err := h.Take(int(ReservationBytes))
		if err != nil || n != int(ReservationBytes) {
			b.Fatal(n, err)
		}
		h.Complete(n, n, true)
	}
}

func BenchmarkSQLiteCommit(b *testing.B) {
	s, _, _ := benchmarkMeter(b, 1)
	if _, err := s.db.Exec(`CREATE TABLE performance_sample(value BLOB); INSERT INTO performance_sample VALUES(zeroblob(4096))`); err != nil {
		b.Fatal(err)
	}
	var samples []int64
	b.ResetTimer()
	for b.Loop() {
		tx, err := s.db.BeginTx(b.Context(), nil)
		if err != nil {
			b.Fatal(err)
		}
		if _, err = tx.Exec(`UPDATE performance_sample SET value=randomblob(4096)`); err != nil {
			tx.Rollback()
			b.Fatal(err)
		}
		start := time.Now()
		err = tx.Commit()
		samples = append(samples, time.Since(start).Nanoseconds())
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	slices.Sort(samples)
	b.ReportMetric(float64(samples[len(samples)/2]), "commit-p50-ns")
	b.ReportMetric(float64(samples[(len(samples)-1)*95/100]), "commit-p95-ns")
}

func TestSQLiteWriterWait(t *testing.T) {
	s, path := fixture(t, 1<<30)
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tx, err := s.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE accounts SET charged=charged WHERE id='alice'`); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	start := time.Now()
	go func() {
		_, e := other.Reserve(t.Context(), "waiting", "alice", "normal", ReservationBytes, time.Now())
		finished <- e
	}()
	select {
	case err := <-finished:
		t.Fatal("writer bypassed lock", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
	t.Logf("SQLite writer wait including 20ms deliberate contention: %.3f ms", float64(time.Since(start).Microseconds())/1000)
}
