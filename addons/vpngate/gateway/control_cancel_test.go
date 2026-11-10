package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/synctest"
)

func TestCancelledControlActionDoesNotPublish(t *testing.T) {
	for _, cancelParent := range []bool{false, true} {
		t.Run(map[bool]string{false: "request", true: "daemon"}[cancelParent], func(t *testing.T) {
			cfg := auditConfig(t)
			m := newManager(cfg, cfg.DataDir+"/config.json")
			defer m.sourceClient.CloseIdleConnections()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			parent, request := t.Context(), t.Context()
			if cancelParent {
				parent = ctx
			} else {
				request = ctx
			}
			r := httptest.NewRequestWithContext(request, http.MethodPost, "/v1/actions", strings.NewReader(`{"op":"policy","require_isp":true}`))
			m.handler(parent).ServeHTTP(httptest.NewRecorder(), r)
			if m.cfg.RequireISP != cfg.RequireISP {
				t.Fatal("cancelled action changed live policy")
			}
			if _, err := os.Stat(m.configPath); !os.IsNotExist(err) {
				t.Fatalf("cancelled action wrote configuration: %v", err)
			}
		})
	}
}

func TestControlQueueCancellation(t *testing.T) {
	for _, cancelParent := range []bool{false, true} {
		t.Run(map[bool]string{false: "request", true: "daemon"}[cancelParent], func(t *testing.T) {
			cfg := auditConfig(t)
			m := newManager(cfg, cfg.DataDir+"/config.json")
			defer m.sourceClient.CloseIdleConnections()
			synctest.Test(t, func(t *testing.T) {
				m.actionSlot = make(chan struct{}, 1)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				parent, request := t.Context(), t.Context()
				if cancelParent {
					parent = ctx
				} else {
					request = ctx
				}
				m.actionSlot <- struct{}{} // Another action owns the publication slot.
				done := make(chan struct{})
				response := httptest.NewRecorder()
				go func() {
					defer close(done)
					r := httptest.NewRequestWithContext(request, http.MethodPost, "/v1/actions", strings.NewReader(`{"op":"policy","require_isp":true}`))
					m.handler(parent).ServeHTTP(response, r)
				}()
				synctest.Wait()
				cancel()
				synctest.Wait()
				select {
				case <-done:
				default:
					t.Error("cancelled request retained its waiting handler")
				}
				<-m.actionSlot
				<-done
				if response.Code != http.StatusServiceUnavailable || m.cfg.RequireISP != cfg.RequireISP {
					t.Fatalf("cancelled queued action succeeded: status=%d policy=%v", response.Code, m.cfg.RequireISP)
				}
			})
			if _, err := os.Stat(m.configPath); !os.IsNotExist(err) {
				t.Fatalf("cancelled queued action wrote configuration: %v", err)
			}
		})
	}
}
