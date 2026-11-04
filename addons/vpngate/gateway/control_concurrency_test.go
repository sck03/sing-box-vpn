package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentControlActionsPreserveIndependentSettings(t *testing.T) {
	for trial := 0; trial < 10; trial++ {
		cfg := auditConfig(t)
		m := newManager(cfg, cfg.DataDir+"/config.json")
		handler := m.handler(context.Background())
		start := make(chan struct{})
		var done sync.WaitGroup
		for _, body := range []string{
			"{\"op\":\"mode\",\"mode\":\"fixed\"}",
			"{\"op\":\"policy\",\"require_isp\":true}",
			"{\"op\":\"policy\",\"exclude_hosting\":false}",
		} {
			done.Add(1)
			go func(body string) {
				defer done.Done()
				<-start
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/actions", strings.NewReader(body)))
				if response.Code != http.StatusAccepted {
					t.Errorf("control action failed: %s", response.Body)
				}
			}(body)
		}
		close(start)
		done.Wait()
		m.mu.Lock()
		current := m.cfg
		m.mu.Unlock()
		m.sourceClient.CloseIdleConnections()
		if current.Mode != "fixed" || !current.RequireISP || current.ExcludeHosting {
			t.Fatalf("concurrent action lost another setting on trial %d: mode=%s require_isp=%v exclude_hosting=%v", trial, current.Mode, current.RequireISP, current.ExcludeHosting)
		}
	}
}
