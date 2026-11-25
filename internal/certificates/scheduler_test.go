package certificates

import (
	"context"
	"errors"
	"testing"
	"time"
)

type renewalFixture struct {
	revision      int64
	assets        []CertificateAsset
	attempts      map[string]time.Time
	queued        []string
	loads, checks int
	queueErr      error
}

func (f *renewalFixture) AppliedCertificates(_ context.Context, since int64) (int64, []CertificateAsset, error) {
	if since == f.revision {
		return f.revision, nil, nil
	}
	f.loads++
	return f.revision, f.assets, nil
}
func (f *renewalFixture) RenewalAttempt(_ context.Context, id string) (time.Time, error) {
	f.checks++
	return f.attempts[id], nil
}
func (f *renewalFixture) QueueRenewal(_ context.Context, id string, now time.Time) error {
	if f.queueErr != nil {
		return f.queueErr
	}
	f.queued = append(f.queued, id)
	f.attempts[id] = now
	return nil
}
func (*renewalFixture) RegistrationReady(CertificateAsset, Material) bool { return true }

func TestSchedulerDeadlinesRevisionAndBackoff(t *testing.T) {
	now := time.Now()
	f := &renewalFixture{revision: 1, assets: []CertificateAsset{{ID: "live", Provider: "acme"}, {ID: "manual", Provider: "import"}}, attempts: map[string]time.Time{}}
	var s Scheduler
	if err := s.Run(t.Context(), f, now); err != nil {
		t.Fatal(err)
	}
	if len(f.queued) != 1 || f.queued[0] != "live" {
		t.Fatal(f.queued)
	}
	if err := s.Run(t.Context(), f, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if f.loads != 1 || f.checks != 1 {
		t.Fatal("idle scan reread assets or attempts", f.loads, f.checks)
	}
	if err := s.Run(t.Context(), f, now.Add(11*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(f.queued) != 1 {
		t.Fatal("renewal backoff was ignored")
	}
	f.revision++
	f.assets = []CertificateAsset{{ID: "new", Provider: "acme"}}
	if err := s.Run(t.Context(), f, now.Add(12*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(f.queued) != 2 || f.queued[1] != "new" {
		t.Fatal("new applied revision did not bypass scan deadline", f.queued)
	}
	if err := s.Run(t.Context(), f, now.Add(13*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(f.queued) != 3 {
		t.Fatal("backoff never expired")
	}
}

func TestSchedulerRetriesQueueFailureWithoutWaitingForDeadline(t *testing.T) {
	now := time.Now()
	want := errors.New("queue unavailable")
	f := &renewalFixture{revision: 1, assets: []CertificateAsset{{ID: "live", Provider: "acme"}}, attempts: map[string]time.Time{}, queueErr: want}
	var s Scheduler
	if err := s.Run(t.Context(), f, now); !errors.Is(err, want) {
		t.Fatal(err)
	}
	f.queueErr = nil
	if err := s.Run(t.Context(), f, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(f.queued) != 1 || f.loads != 1 {
		t.Fatal("failed work was not retried from the cached registry", f)
	}
}
