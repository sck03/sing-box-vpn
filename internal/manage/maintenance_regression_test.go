package manage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestPublishedCredentialsDoNotPollStoppedServices(t *testing.T) {
	s, err := OpenPlatform(t.TempDir(), "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	if err = s.Accounts.SaveAccount(ctx, domain.Account{ID: "a", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Accounts.BindBatch(ctx, []domain.Credential{{ID: "c", AccountID: "a", RuntimeID: "normal", InboundID: "in", Protocol: "vless", Generation: 1, UUID: randomUUID()}}); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.Accounts.HasUnpublishedCredentials(ctx); err != nil || !pending {
		t.Fatal(pending, err)
	}
	if err = s.Accounts.CredentialsPublished(ctx, "normal", 1); err != nil {
		t.Fatal(err)
	}
	version := s.statusVersion
	for range 3 {
		if pending, err := s.Accounts.HasUnpublishedCredentials(ctx); err != nil || pending {
			t.Fatal(pending, err)
		}
		// No systemctl, configuration, runtime binary or worker is available here.
		if err = s.reconcileCredentials(ctx); err != nil {
			t.Fatal(err)
		}
	}
	publication, err := s.Accounts.CredentialPublication(ctx, "normal")
	if err != nil || publication.Loaded != 0 || s.statusVersion != version {
		t.Fatal(publication, s.statusVersion, err)
	}
}

func TestApplyCheckpointKeepsExecutionBaselineOnReplay(t *testing.T) {
	s, err := OpenPlatform(t.TempDir(), "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	if err = s.Store.Initialize(ctx, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	id, err := s.Store.Queue(ctx, "apply", domain.TaskSubmission{Actor: "test", Source: domain.TaskUser}, []byte(`{"revision":2}`), 1, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Store.ClaimLane(ctx, domain.ControlLane)
	if err != nil {
		t.Fatal(err)
	}
	var input applyInput
	if err = json.Unmarshal(task.Input, &input); err != nil || input.Services != nil {
		t.Fatal(input, err)
	}
	if err = s.checkpointApplyServices(ctx, id, &input); err != nil || input.Services == nil {
		t.Fatal(input, err)
	}
	if err = s.Store.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	task, err = s.Store.ClaimLane(ctx, domain.ControlLane)
	if err != nil {
		t.Fatal(err)
	}
	input = applyInput{}
	if err = json.Unmarshal(task.Input, &input); err != nil || input.Services == nil {
		t.Fatal(input, err)
	}
	// Replay must retain the saved baseline without trying to sample systemd.
	s.NoSystem = false
	if err = s.checkpointApplyServices(ctx, id, &input); err != nil {
		t.Fatal(err)
	}
}

func TestControlLaneClaimsWhileMaintenanceLockIsBusy(t *testing.T) {
	s, err := OpenPlatform(t.TempDir(), "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Store.Initialize(t.Context(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.Accounts.SaveAccount(t.Context(), domain.Account{ID: "a", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Accounts.BindBatch(t.Context(), []domain.Credential{{ID: "c", AccountID: "a", RuntimeID: "normal", InboundID: "in", Protocol: "vless", Generation: 1, UUID: randomUUID()}}); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(s.Root, ".service-recovery.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = lockFile(lock); err != nil {
		t.Fatal(err)
	}
	id, err := s.Store.QueueTask(t.Context(), domain.ControlLane, "program-upgrade", domain.TaskSubmission{Actor: "test", Source: domain.TaskUser}, []byte(`{"operation":"program-upgrade"}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	done := make(chan error, 1)
	go func() { done <- s.runWorkers(ctx) }()
	defer func() { cancel(); <-done }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		task, err := s.Store.Task(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if task.State == "failed" {
			if !strings.Contains(task.Error, "requires Linux systemd") {
				t.Fatal(task.Error)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("control lane waited for unrelated maintenance", ctx.Err())
		case <-ticker.C:
		}
	}
}
