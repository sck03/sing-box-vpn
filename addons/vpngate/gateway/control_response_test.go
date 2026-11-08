package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestControlResponseBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, body, wantError string
		status                int
		chunked, truncated    bool
	}{
		{name: "exact limit", status: 200, body: strings.Repeat("x", 1<<20)},
		{name: "known oversized", status: 200, body: strings.Repeat("x", (1<<20)+1), wantError: "too large"},
		{name: "chunked oversized", status: 200, body: strings.Repeat("x", (1<<20)+1), chunked: true, wantError: "too large"},
		{name: "truncated", status: 200, body: `{"state":"ready"}`, truncated: true, wantError: "unexpected EOF"},
		{name: "redirect", status: 302, body: "redirected", wantError: "302"},
		{name: "policy error", status: 400, body: `{"error":"invalid policy"}`, wantError: "invalid policy"},
		{name: "missing error", status: 503, body: `{}`, wantError: "503"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			listener, err := net.Listen("unix", filepath.Join(dir, "control.sock"))
			if err != nil {
				if runtime.GOOS == "windows" {
					t.Skipf("Unix sockets unavailable: %v", err)
				}
				t.Fatal(err)
			}
			defer listener.Close()
			cfg := auditConfig(t)
			cfg.RunDir = dir
			path := filepath.Join(dir, "gateway.json")
			if err := atomicJSON(path, cfg); err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/status" {
					_, _ = w.Write([]byte("redirect was followed"))
					return
				}
				if test.status == http.StatusFound {
					w.Header().Set("Location", "/redirect-target")
				}
				if !test.chunked {
					size := len(test.body)
					if test.truncated {
						size++
					}
					w.Header().Set("Content-Length", fmt.Sprint(size))
				}
				w.WriteHeader(test.status)
				if test.chunked {
					w.(http.Flusher).Flush()
				}
				_, _ = w.Write([]byte(test.body))
			})}
			defer server.Close()
			go server.Serve(listener)
			var output bytes.Buffer
			err = runCTLWithOutput([]string{"status", "--config", path}, &output)
			if test.wantError == "" {
				if err != nil || output.String() != test.body {
					t.Fatalf("valid response failed: %v, output length %d", err, output.Len())
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantError) || output.Len() != 0 {
				t.Fatalf("got %v, output length %d; want %q and no output", err, output.Len(), test.wantError)
			}
		})
	}
}
