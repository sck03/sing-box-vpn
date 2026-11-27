package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"github.com/sck03/sing-box-vpn/internal/hostservice"
)

func TestTunnelSyncContinuesWithFailedDesiredRevision(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, _ := platformFixture(t, root, core)
	for _, node := range config.App.Nodes {
		if node.Protocol == "vmess" {
			config.Argo = TunnelSettings{Mode: "temporary", Inbound: node.ID}
		}
	}
	if err := applyFixture(t, service, config, 1); err != nil {
		t.Fatal(err)
	}
	previous := tunnelObservation{Settings: config.Argo, Runtime: hostservice.Tunnel{Invocation: strings.Repeat("a", 32), Domain: "old.trycloudflare.com"}}
	if err := service.Store.Observe(t.Context(), "argo-tunnel", string(mustJSON(t, previous))); err != nil {
		t.Fatal(err)
	}
	config.Argo = TunnelSettings{Mode: "off"}
	id, err := service.Store.Queue(t.Context(), "apply", domain.TaskSubmission{Actor: "test", Source: domain.TaskUser}, []byte("{}"), 2, mustJSON(t, config))
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Store.Finish(t.Context(), id, errors.New("activation failed")); err != nil {
		t.Fatal(err)
	}
	before, err := service.Store.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	run := func(_ context.Context, name string, _ ...string) ([]byte, error) {
		calls++
		if name == "systemctl" {
			return []byte("ActiveState=active\nInvocationID=" + strings.Repeat("b", 32)), nil
		}
		return []byte("https://new.trycloudflare.com"), nil
	}
	version := service.statusVersion
	if err := service.syncTunnelWith(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	applied, _, err := service.appliedConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	applied, err = observedConfig(t.Context(), service.Store, applied)
	if err != nil || applied.tunnelDomain != "new.trycloudflare.com" || calls != 2 {
		t.Fatal(applied.tunnelDomain, calls, err)
	}
	after, err := service.Store.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.Desired, after.Desired) || before.Revision != after.Revision || after.AppliedRevision != 2 {
		t.Fatal("observation changed desired/applied configuration")
	}
	if service.statusVersion == version {
		t.Fatal("observation did not invalidate status")
	}
}

func TestOverviewInvalidatesBeyondTaskSummary(t *testing.T) {
	service, err := OpenPlatform(t.TempDir(), "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if err = service.Store.Initialize(t.Context(), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	var hidden int64
	for i := 0; i < 13; i++ {
		id, err := service.Store.QueueTask(t.Context(), "subscription", "subscription", domain.TaskSubmission{Actor: "test", Source: domain.TaskUser}, []byte("{}"))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			hidden = id
		}
	}
	read := func() OverviewSnapshot {
		data, err := service.overviewSnapshot(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		var result OverviewSnapshot
		if err = json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := read()
	for _, task := range before.Tasks {
		if task.ID == hidden {
			t.Fatal("fixture task is visible")
		}
	}
	if err := service.finishTask(controlstore.Task{ID: hidden, Kind: "subscription", Actor: "test"}, nil); err != nil {
		t.Fatal(err)
	}
	after := read()
	if !bytes.Equal(mustJSON(t, before.Tasks), mustJSON(t, after.Tasks)) {
		t.Fatal("task summaries should be unchanged")
	}
	if before.ResourceVersion == after.ResourceVersion {
		t.Fatal("hidden completion did not invalidate resources")
	}
}

func TestStartupRecoveryDoesNotWaitForRestartHelper(t *testing.T) {
	root, core, _ := appFixture(t)
	service, _, _ := platformFixture(t, root, core)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := withServiceLock(ctx, root, func() error { return service.recoverApplied(ctx) }); err != nil {
		t.Fatal("startup recovery blocked on the restart helper's service lock:", err)
	}
}
