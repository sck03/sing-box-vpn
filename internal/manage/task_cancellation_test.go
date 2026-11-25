package manage

import (
	"context"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/application"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/controlstore"
)

func TestProbeCancellationBeforeAndAfterRegistration(t *testing.T) {
	for _, before := range []bool{true, false} {
		t.Run(map[bool]string{true: "before", false: "during"}[before], func(t *testing.T) {
			store, err := controlstore.Open(filepath.Join(t.TempDir(), "control.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err = store.Initialize(t.Context(), []byte("{}")); err != nil {
				t.Fatal(err)
			}
			service := &PlatformService{Store: store}
			id, err := store.QueueTask(t.Context(), "probe", "outbound-probe", "admin", []byte("{}"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.ClaimLane(t.Context(), "probe"); err != nil {
				t.Fatal(err)
			}
			if before {
				if err = service.CancelTask(t.Context(), id); err != nil {
					t.Fatal(err)
				}
			}
			started := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- service.runProbe(t.Context(), id, func(ctx context.Context) error { close(started); <-ctx.Done(); return ctx.Err() })
			}()
			if !before {
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("probe did not start")
				}
				if err = service.CancelTask(t.Context(), id); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err = <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("probe did not stop")
			}
			service.probes.mu.Lock()
			remaining := len(service.probes.cancel)
			service.probes.mu.Unlock()
			if remaining != 0 {
				t.Fatal("retained completed probe")
			}
		})
	}
}

func TestRunningOutboundCancellationStopsNetworkAndFinalizes(t *testing.T) {
	root, core, _ := appFixture(t)
	service, _, _ := platformFixture(t, root, core)
	reached := make(chan struct{})
	disconnected := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(reached)
		<-r.Context().Done()
		close(disconnected)
	}))
	defer server.Close()
	id, err := service.Queue(t.Context(), application.TaskCommand{Operation: "outbound-probe", ID: "direct", Value: server.URL}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.Store.ClaimLane(t.Context(), application.ProbeLane)
	if err != nil || task.ID != id {
		t.Fatal(task, err)
	}
	done := make(chan error, 1)
	go func() { done <- service.runTask(t.Context(), task) }()
	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		t.Fatal("probe did not reach local endpoint")
	}
	if err = service.CancelTask(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled probe retained worker")
	}
	select {
	case <-disconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled probe retained network connection")
	}
	final, err := service.Store.Task(t.Context(), id)
	if err != nil || final.State != "cancelled" || final.Cancellable {
		t.Fatal(final, err)
	}
	files, err := filepath.Glob(filepath.Join(root, ".probe-*"))
	if err != nil || len(files) != 0 {
		t.Fatal("probe retained temporary files", files, err)
	}
}
