package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLiveStatus(t *testing.T) {
	dir := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(dir, "control.sock"))
	if err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("Unix sockets unavailable on this Windows host: %v", err)
		}
		t.Fatal(err)
	}
	defer listener.Close()
	config := defaults()
	config.Country, config.Enabled = "JP", true
	config.ProxyUser, config.ProxyPass = "test", strings.Repeat("private", 8)
	config.DataDir, config.RunDir = filepath.Join(dir, "data"), dir
	path := filepath.Join(dir, "gateway.json")
	if err := atomicJSON(path, config); err != nil {
		t.Fatal(err)
	}
	var body atomic.Value
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/status" {
			http.Error(w, "read-only status expected", http.StatusBadRequest)
			return
		}
		_, _ = w.Write(body.Load().([]byte))
	})}
	defer server.Close()
	go server.Serve(listener)
	for _, state := range []string{"ready", "stopped"} {
		payload, err := json.Marshal(Status{State: state, Country: "JP", Mode: "fixed", Exit: &Geo{IP: "203.0.113.20", Country: "JP"}})
		if err != nil {
			t.Fatal(err)
		}
		body.Store(payload)
		var out bytes.Buffer
		if err := runCTLWithOutput([]string{"status", "--config", path}, &out); err != nil || !bytes.Equal(out.Bytes(), payload) {
			t.Fatal("JSON control response changed", err, out.String())
		}
		out.Reset()
		if err := runCTLWithOutput([]string{"status", "--config", path, "--format", "text", "--timeout", "2s"}, &out); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "203.0.113.20") != (state == "ready") {
			t.Fatal("live state was not reflected in the text view", out.String())
		}
	}
}

func TestGatewayDisplayRequiresLiveVerifiedExit(t *testing.T) {
	for _, test := range []struct {
		name, state, country string
		exit                 *Geo
		connected            bool
	}{
		{"ready", "ready", "JP", &Geo{IP: "203.0.113.20", Country: "JP"}, true},
		{"connecting with old exit", "connecting", "JP", &Geo{IP: "203.0.113.20", Country: "JP"}, false},
		{"stopped with old exit", "stopped", "JP", &Geo{IP: "203.0.113.20", Country: "JP"}, false},
		{"wrong country", "ready", "JP", &Geo{IP: "203.0.113.20", Country: "KR"}, false},
		{"missing exit", "ready", "JP", nil, false},
		{"invalid IP", "ready", "JP", &Geo{IP: "invalid", Country: "JP"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			status := Status{State: test.state, Country: test.country, Mode: "sticky", Exit: test.exit}
			if err := writeGatewayStatus(&out, status); err != nil {
				t.Fatal(err)
			}
			text := out.String()
			if strings.Contains(text, "已连接（出口已验证）") != test.connected || strings.Contains(text, "203.0.113.20") != test.connected {
				t.Fatal("unverified or old exit presented as connected", text)
			}
			if !strings.Contains(text, "日本 / JP") || !strings.Contains(text, "故障自动重连") {
				t.Fatal("country or connection mode missing", text)
			}
		})
	}
}

func TestGatewayDisplayEscapesRemoteText(t *testing.T) {
	var out bytes.Buffer
	status := Status{State: "waiting", Country: "JP", Mode: "fixed", Node: "node\x1b[31m\r", Reason: "bad\u202e\x1b[2J"}
	if err := writeGatewayStatus(&out, status); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.String(), "\x1b\r\u202e") || !strings.Contains(out.String(), "锁定当前节点") {
		t.Fatal("unsafe terminal text", out.String())
	}
}

func TestOfflineGatewayDisplayDoesNotReadCachedExit(t *testing.T) {
	dir := t.TempDir()
	config := defaults()
	config.Country, config.Enabled = "KR", true
	config.ProxyUser, config.ProxyPass = "preview", strings.Repeat("private", 8)
	config.DataDir, config.RunDir = filepath.Join(dir, "data"), filepath.Join(dir, "run")
	path := filepath.Join(dir, "gateway.json")
	if err := atomicJSON(path, config); err != nil {
		t.Fatal(err)
	}
	stale := Status{State: "ready", Country: "KR", Exit: &Geo{IP: "203.0.113.20", Country: "KR"}}
	if err := atomicJSON(filepath.Join(config.DataDir, "state.json"), stale); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runCTLWithOutput([]string{"status", "--config", path, "--format", "text", "--timeout", "50ms"}, &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "状态不可用") || !strings.Contains(text, "韩国 / KR") || strings.Contains(text, "203.0.113.20") || strings.Contains(text, config.ProxyPass) {
		t.Fatal("offline status is misleading or leaks credentials", text)
	}
	out.Reset()
	if err := runCTLWithOutput([]string{"status", "--config", path, "--timeout", "50ms"}, &out); err == nil || out.Len() != 0 {
		t.Fatal("default JSON control behavior changed", err, out.String())
	}
}
