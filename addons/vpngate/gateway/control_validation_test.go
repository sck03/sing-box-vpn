package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestInvalidControlBodyDoesNotApplyAction(t *testing.T) {
	for _, body := range []string{
		`{"op":"stop"} {"op":"start"}`,
		`{"op":"stop"} garbage`,
		`{"op":"stop"}` + strings.Repeat(" ", 16<<10),
	} {
		cfg := auditConfig(t)
		path := cfg.DataDir + "/config.json"
		m := newManager(cfg, path)
		response := httptest.NewRecorder()
		m.handler(context.Background()).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/actions", strings.NewReader(body)))
		m.sourceClient.CloseIdleConnections()
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid action body was accepted: %d", response.Code)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("invalid request changed the configuration", err)
		}
	}
}

func TestControlBodyAtSizeLimit(t *testing.T) {
	cfg := auditConfig(t)
	m := newManager(cfg, cfg.DataDir+"/config.json")
	defer m.sourceClient.CloseIdleConnections()
	body := `{"op":"stop"}`
	body += strings.Repeat(" ", (16<<10)-len(body))
	response := httptest.NewRecorder()
	m.handler(context.Background()).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/actions", strings.NewReader(body)))
	if response.Code != http.StatusAccepted {
		t.Fatalf("complete action at the size limit was rejected: %d", response.Code)
	}
}

type shortSOCKSReplyWriter struct{}

func (shortSOCKSReplyWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestSOCKSReplyReportsShortWrite(t *testing.T) {
	if err := socksReply(shortSOCKSReplyWriter{}, 0, net.IPv4(127, 0, 0, 1), 12345); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("incomplete SOCKS reply was reported as successful", err)
	}
}
