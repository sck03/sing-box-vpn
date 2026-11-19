package httpserver

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimitedServerPreservesTLSAndHTTP2(t *testing.T) {
	certificate := httptest.NewTLSServer(http.NotFoundHandler())
	client := certificate.Client()
	client.Timeout = 5 * time.Second
	transport := client.Transport.(*http.Transport)
	transport.ForceAttemptHTTP2 = true
	defer transport.CloseIdleConnections()
	config := certificate.TLS.Clone()
	config.NextProtos = nil
	certificate.Close()
	socket, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{TLSConfig: config, ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "%t/%d", r.TLS != nil, r.ProtoMajor) })}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, socket, server, 2) }()
	response, err := client.Get("https://" + socket.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(data) != "true/2" {
		t.Fatalf("TLS/HTTP2 state: %s %v", data, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
