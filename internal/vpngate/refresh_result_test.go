package vpngate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshControlReportsCompletionAndCache(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "cached"}[cached], func(t *testing.T) {
			cfg := defaults()
			cfg.DataDir = t.TempDir()
			cfg.Sources = []string{"https://example.invalid/nodes?token=synthetic-secret"}
			m := newTestManager(cfg, "")
			defer m.sourceClient.CloseIdleConnections()
			initial, err := json.Marshal(m.snapshot())
			if err != nil || bytes.Contains(initial, []byte(`"last_success"`)) {
				t.Fatal("new managers must not report a fabricated success time", string(initial), err)
			}
			if cached {
				m.nodes = []Node{{Host: "saved-node"}}
			}
			entered, release := make(chan struct{}), make(chan struct{})
			m.sourceClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				close(entered)
				<-release
				return nil, errors.New("failed URL " + r.URL.String())
			})
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				defer close(done)
				m.handler(t.Context()).ServeHTTP(response, httptest.NewRequest("POST", "/v1/actions", strings.NewReader(`{"op":"refresh"}`)))
			}()
			<-entered
			select {
			case <-done:
				t.Error("refresh returned before fetching completed")
			default:
			}
			if m.snapshot().SourceRefresh.Outcome != "running" {
				t.Error("missing running state")
			}
			// Refresh I/O must not monopolize the control slot used by stop/reload.
			select {
			case m.actionSlot <- struct{}{}:
				<-m.actionSlot
			default:
				t.Error("refresh blocked gateway controls")
			}
			close(release)
			<-done
			wantCode, wantOutcome := http.StatusBadGateway, "failed"
			if cached {
				wantCode, wantOutcome = http.StatusOK, "cached"
			}
			var result RefreshStatus
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != wantCode || result.Outcome != wantOutcome || result.Cached != cached || result.FailedSources != 1 {
				t.Fatal(response.Code, result)
			}
			if bytes.Contains(response.Body.Bytes(), []byte("synthetic-secret")) {
				t.Fatal("source credential leaked")
			}
			if m.snapshot().SourceRefresh.Outcome != wantOutcome {
				t.Fatal("status and task result disagree")
			}
			m.sourceClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(benchmarkNodes(1)))}, nil
			})
			nodes, result := m.refreshResult(context.Background(), cfg, true)
			if result.Outcome != "passed" || result.Cached || result.LastSuccess == nil || result.LastSuccess.IsZero() || len(nodes) != 1 {
				t.Fatal(result)
			}
			lastSuccess := *result.LastSuccess
			m.sourceClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("source unavailable")
			})
			_, result = m.refreshResult(t.Context(), cfg, true)
			if result.Outcome != "cached" || result.LastSuccess == nil || !result.LastSuccess.Equal(lastSuccess) {
				t.Fatal("failed refresh lost the last successful fetch", result)
			}
		})
	}
}

func TestRefreshReusePreservesCacheWriteFailure(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	cfg.Sources = []string{"https://example.invalid/nodes"}
	m := newTestManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	// A directory at the cache filename makes atomic replacement fail on all hosts.
	if err := os.Mkdir(filepath.Join(cfg.DataDir, "nodes.json"), 0700); err != nil {
		t.Fatal(err)
	}
	reads := 0
	m.sourceClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		reads++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(benchmarkNodes(1)))}, nil
	})
	_, first := m.refreshResult(t.Context(), cfg, true)
	nodes, reused := m.refreshResult(t.Context(), cfg, false)
	if first.Outcome != "partial" || reused != first || reads != 1 || len(nodes) != 1 {
		t.Fatal("reuse hid the cache persistence failure", first, reused, reads, len(nodes))
	}
}
