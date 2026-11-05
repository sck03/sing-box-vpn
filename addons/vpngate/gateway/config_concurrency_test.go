package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/manage"
)

func TestControlActionPreservesPublishedMenuConfiguration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "vpngate", "gateway.json")
	cfg := defaults()
	cfg.ProxyUser, cfg.ProxyPass = "old", strings.Repeat("a", 32)
	cfg.DataDir, cfg.RunDir = t.TempDir(), t.TempDir()
	if err := atomicJSON(path, cfg); err != nil {
		t.Fatal(err)
	}
	m := newManager(cfg, path)
	defer m.sourceClient.CloseIdleConnections()
	latest := cfg
	latest.Country, latest.ProxyUser, latest.ProxyPass = "KR", "new", strings.Repeat("b", 32)
	latest.Sources = []string{"https://mirror.example/api/iphone/"}
	if err := manage.ConfigTransaction(root, func() error { return atomicJSON(path, latest) }); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	m.handler(context.Background()).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/actions", strings.NewReader(`{"op":"policy","require_isp":true}`)))
	if response.Code != http.StatusAccepted {
		t.Fatal(response.Code, response.Body.String())
	}
	latest.RequireISP = true
	persisted, err := readConfig(path)
	if err != nil || !reflect.DeepEqual(persisted, latest) || !reflect.DeepEqual(m.cfg, latest) {
		t.Fatal("daemon action overwrote menu country, credentials or sources", persisted, err)
	}
}

func TestControlActionDoesNotWriteDuringConfigTransaction(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "vpngate", "gateway.json")
	cfg := defaults()
	cfg.ProxyUser, cfg.ProxyPass = "test", strings.Repeat("a", 32)
	cfg.DataDir, cfg.RunDir = t.TempDir(), t.TempDir()
	if err := atomicJSON(path, cfg); err != nil {
		t.Fatal(err)
	}
	m := newManager(cfg, path)
	defer m.sourceClient.CloseIdleConnections()
	if err := manage.ConfigTransaction(root, func() error {
		response := httptest.NewRecorder()
		m.handler(context.Background()).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/actions", strings.NewReader(`{"op":"policy","require_isp":true}`)))
		if response.Code != http.StatusBadRequest {
			t.Fatal("control action bypassed the active configuration writer", response.Code)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	persisted, err := readConfig(path)
	if err != nil || !reflect.DeepEqual(persisted, cfg) || !reflect.DeepEqual(m.cfg, cfg) {
		t.Fatal("blocked action altered configuration", persisted, err)
	}
}
