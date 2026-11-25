package manage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestRefreshValidationDoesNotHoldPublicationLock(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "download", true: "stale-config"}[conflict], func(t *testing.T) {
			root, core, _ := appFixture(t)
			config, _, err := loadSubscriptionState(root)
			if err != nil {
				t.Fatal(err)
			}
			entered, resume := make(chan struct{}), make(chan struct{})
			confirmSubscriptionFixture(t, root)
			compiler := func(ctx context.Context, root, core string, c sub.Config, r sub.Runtime, local map[string][]sub.Node, mode sub.BuildMode) (fileBundle, sub.Snapshot, map[string]sub.Preview, error) {
				close(entered)
				select {
				case <-resume:
				case <-ctx.Done():
					return nil, sub.Snapshot{}, nil, ctx.Err()
				}
				return compilePreparedSubscriptions(ctx, root, core, c, r, local, mode)
			}
			done := make(chan error, 1)
			go func() {
				_, err := publishSubscriptionRefresh(t.Context(), root, core, config, nil, nil, nil, time.Now(), false, &application.RefreshResult{}, compiler)
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("compiler did not start")
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			err = withStartupLock(ctx, root, func() error {
				if conflict {
					config.Revision++
					return AtomicJSON(filepath.Join(root, subscriptionsConfig), config)
				}
				return nil
			})
			cancel()
			close(resume)
			if err != nil {
				t.Fatal("reader was blocked by core validation", err)
			}
			err = <-done
			if conflict && !errors.Is(err, application.ErrConflict) || !conflict && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSubscriptionHeadersStayOnTheirOrigin(t *testing.T) {
	seen := make(chan bool, 2)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen <- true; w.Write([]byte("not expected")) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" || r.UserAgent() != "test-agent" {
			t.Error("source headers missing")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, target.URL, http.StatusFound)
			return
		}
		w.Write([]byte("hysteria2://secret@example.com:443#node"))
	}))
	defer origin.Close()
	client := origin.Client()
	source := sub.Source{ID: "source", Kind: "remote", Format: "auto", URL: origin.URL, UserAgent: "test-agent", Headers: map[string]string{"Authorization": "Bearer test-token"}}
	cache := refreshSource(t.Context(), client, source, sub.SourceCache{}, time.Now())
	if cache.Error != "" || len(cache.Nodes) != 1 {
		t.Fatal(cache.Error)
	}
	source.URL = origin.URL + "/redirect"
	cache = refreshSource(t.Context(), client, source, sub.SourceCache{}, time.Now())
	if cache.Error == "" {
		t.Fatal("credential-bearing redirect accepted")
	}
	select {
	case <-seen:
		t.Fatal("credentials sent to different origin")
	default:
	}
}
