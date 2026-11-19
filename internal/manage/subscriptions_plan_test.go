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
	command := fixtureSubscriptionCommand{Operation: "apply", Config: mustJSON(t, c)}
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
	for _, secret := range app.fixtureProfile().Secrets.UUID {
		if strings.Contains(result.String(), secret) {
			t.Fatal("preview leaks credential")
		}
	}
	s, c, session := platformFixture(t, root, core)
	handler := s.Handler(PanelOptions{PublicURL: "http://example.com"})
	req := panelRequest("POST", "/api/config", mustJSON(t, Object{"config": c, "revision": 0}), session)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 409 {
		t.Fatal("revision conflict status", w.Code, w.Body.String())
	}
}
