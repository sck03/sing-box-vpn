package controlstore

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func submissionFixture(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err = s.Initialize(t.Context(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	return s, path
}
func userSubmission() domain.TaskSubmission {
	return domain.TaskSubmission{Actor: "scheduler", Source: domain.TaskUser, RequestID: "review-request-0001", Fingerprint: strings.Repeat("a", 64)}
}

func TestTaskSubmissionSeparatesUserAndScheduler(t *testing.T) {
	s, _ := submissionFixture(t)
	user := domain.TaskSubmission{Actor: "scheduler", Source: domain.TaskUser}
	scheduled := domain.TaskSubmission{Actor: "scheduler", Source: domain.TaskScheduler}
	var ids []int64
	for _, identity := range []domain.TaskSubmission{user, user, scheduled, scheduled} {
		id, err := s.QueueTask(t.Context(), domain.ControlLane, "service", identity, []byte(`{}`))
		if err != nil || id < 1 {
			t.Fatal(id, err)
		}
		ids = append(ids, id)
	}
	if ids[0] == ids[1] || ids[1] == ids[2] || ids[2] != ids[3] {
		t.Fatal(ids)
	}
}

func TestTaskSubmissionConcurrentRetryAndRestart(t *testing.T) {
	s, path := submissionFixture(t)
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	identity := userSubmission()
	var wg sync.WaitGroup
	ids := make(chan int64, 12)
	for i := range 12 {
		wg.Go(func() {
			store := s
			if i%2 == 1 {
				store = other
			}
			id, err := store.Queue(t.Context(), "service", identity, []byte(`{}`), 0, nil)
			if err != nil {
				t.Error(err)
			}
			ids <- id
		})
	}
	wg.Wait()
	close(ids)
	var first int64
	for id := range ids {
		if id < 1 || first != 0 && id != first {
			t.Fatal(first, id)
		}
		first = id
	}
	if err = s.Finish(t.Context(), first, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	id, err := reopened.Queue(t.Context(), "service", identity, []byte(`{}`), 0, nil)
	if err != nil || id != first {
		t.Fatal(id, err)
	}
	mismatch := identity
	mismatch.Fingerprint = strings.Repeat("b", 64)
	if _, err = reopened.SubmittedTask(t.Context(), mismatch); !errors.Is(err, ErrRequestConflict) {
		t.Fatal(err)
	}
	if err = reopened.DeleteTask(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Queue(t.Context(), "service", identity, []byte(`{}`), 0, nil); !errors.Is(err, ErrRequestExpired) {
		t.Fatal(err)
	}
}

func TestTaskSubmissionApplyRetryAndCapacityRollback(t *testing.T) {
	s, _ := submissionFixture(t)
	identity := userSubmission()
	id, err := s.Queue(t.Context(), "apply", identity, []byte(`{}`), 1, []byte(`{"saved":true}`))
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Queue(t.Context(), "apply", identity, []byte(`{}`), 1, []byte(`{"saved":true}`))
	if err != nil || again != id {
		t.Fatal(again, err)
	}
	state, err := s.State(t.Context())
	if err != nil || state.Revision != 2 {
		t.Fatal(state, err)
	}
	if err = s.Finish(t.Context(), id, nil); err != nil {
		t.Fatal(err)
	}
	// Receipt capacity failures must roll back both task and desired revision.
	_, err = s.db.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<9999) INSERT INTO task_requests SELECT 'capacity',printf('request-%d',x),'',1,? FROM n`, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	identity.RequestID = "review-request-0002"
	if _, err = s.Queue(t.Context(), "apply", identity, []byte(`{}`), 2, []byte(`{"saved":false}`)); !errors.Is(err, ErrRequestLimit) {
		t.Fatal(err)
	}
	state, err = s.State(t.Context())
	if err != nil || state.Revision != 2 || string(state.Desired) != `{"saved":true}` {
		t.Fatal(state, err)
	}
}

func TestTaskSubmissionRetentionProtectsActiveAndRemovedTasks(t *testing.T) {
	s, _ := submissionFixture(t)
	identity := userSubmission()
	active, err := s.Queue(t.Context(), "service", identity, []byte(`{}`), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity.RequestID = "review-request-0002"
	completed, err := s.Queue(t.Context(), "service", identity, []byte(`{}`), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(t.Context(), completed, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE task_requests SET created=?`, time.Now().Add(-25*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if err = s.PruneHistory(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM task_requests WHERE task_id=?`, active).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM task_requests WHERE task_id=?`, completed).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestTaskSubmissionRetryAtQueueCapacity(t *testing.T) {
	store, _ := submissionFixture(t)
	request := userSubmission()
	first, err := store.Queue(t.Context(), "service", request, []byte(`{}`), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 31 {
		if _, err = store.Queue(t.Context(), "service", domain.TaskSubmission{Actor: "other", Source: domain.TaskUser}, []byte(`{}`), 0, nil); err != nil {
			t.Fatal(err)
		}
	}
	retry, err := store.Queue(t.Context(), "service", request, []byte(`{}`), 0, nil)
	if err != nil || retry != first {
		t.Fatal(retry, err)
	}
}
