package manage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestPlanRejectsConcurrentMutationAfterUnlockedCompile(t *testing.T) {
	root, core, _ := appFixture(t)
	c, _, _, err := loadSubscriptions(root)
	if err != nil {
		t.Fatal(err)
	}
	before := readBytes(t, filepath.Join(root, subscriptionsSnapshot))
	service := SubscriptionService{Root: root, Core: core}
	command := application.SubscriptionCommand{Operation: "apply", Config: mustJSON(t, c)}
	err = service.plan(context.Background(), command, io.Discard, func(ctx context.Context, root, core string, c sub.Config, r sub.Runtime, local map[string][]sub.Node) (fileBundle, sub.Snapshot, map[string]sub.Preview, error) {
		child, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := withStartupLock(child, root, func() error {
			latest, _, _, err := loadSubscriptions(root)
			if err != nil {
				return err
			}
			latest.Revision += 10
			return AtomicJSON(filepath.Join(root, subscriptionsConfig), latest)
		}); err != nil {
			t.Fatal("compile held global lock", err)
		}
		return buildSubscriptions(ctx, root, core, c, r, local)
	})
	if !errors.Is(err, application.ErrConflict) {
		t.Fatal("concurrent edit was overwritten", err)
	}
	if !bytes.Equal(before, readBytes(t, filepath.Join(root, subscriptionsSnapshot))) {
		t.Fatal("rejected plan published artifacts")
	}
}

func TestPreviewSummaryAndHTTPConflict(t *testing.T) {
	root, core, app := appFixture(t)
	var result bytes.Buffer
	if err := RunSubscriptions(context.Background(), []string{"--root", root, "--core", core, "preview"}, nil, &result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.String(), `"data"`) || strings.Contains(result.String(), `"nodes"`) {
		t.Fatal("preview contains full artifacts or credentials")
	}
	for _, secret := range app.Profile.Secrets.UUID {
		if strings.Contains(result.String(), secret) {
			t.Fatal("preview leaks credential")
		}
	}
	c, _, _, _ := loadSubscriptions(root)
	stale := c
	c.Revision++
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), c); err != nil {
		t.Fatal(err)
	}
	handler := subscriptionAdminHandler(root, core, strings.Repeat("b", 43))
	req := httptest.NewRequest("POST", "/api/apply", bytes.NewReader(mustJSON(t, adminRequest{Config: mustJSON(t, stale)})))
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("b", 43))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 409 {
		t.Fatal("revision conflict status", w.Code, w.Body.String())
	}
}

func TestScheduledTasksSeparateCancellationAndDeadlines(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var refresh int
		done := make(chan struct{})
		go func() {
			defer close(done)
			runScheduledTasks(ctx, time.Second, 10*time.Second,
				func(context.Context) { refresh++ }, func(ctx context.Context) { <-ctx.Done() },
			)
		}()
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if refresh < 3 {
			t.Fatal("blocked delivery postponed refresh", refresh)
		}
		cancel()
		<-done
	})
}
