package httpserver

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestSubscriptionConnectionLimitAndShutdown(t *testing.T) {
	base, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &listener{Listener: base, slots: make(chan struct{}, 1), done: make(chan struct{})}
	defer listener.Close()
	client, err := net.Dial("tcp4", base.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	first, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	result := make(chan error, 1)
	go func() {
		c, err := listener.Accept()
		if c != nil {
			c.Close()
		}
		result <- err
	}()
	select {
	case <-result:
		t.Fatal("capacity exceeded")
	case <-time.After(20 * time.Millisecond):
	}
	listener.Close()
	select {
	case err := <-result:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked shutdown")
	}
	first.Close()
	first.Close()
	if len(listener.slots) != 0 {
		t.Fatal("capacity leak")
	}
}

type failingSubscriptionListener struct {
	net.Listener
	fail     <-chan struct{}
	accepted bool
}

func (l *failingSubscriptionListener) Accept() (net.Conn, error) {
	if l.accepted {
		<-l.fail
		return nil, errors.New("listener failed")
	}
	l.accepted = true
	return l.Listener.Accept()
}

func TestSubscriptionListenerFailureClosesActiveRequests(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	started, stopped := make(chan struct{}), make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(stopped) })}
	defer server.Close()
	fail := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- Serve(context.Background(), &failingSubscriptionListener{Listener: listener, fail: fail}, server, 32)
	}()
	client, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		close(fail)
		t.Fatal(err)
	}
	defer client.Close()
	io.WriteString(client, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(fail)
		t.Fatal("not started")
	}
	close(fail)
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("failure hidden")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("not stopped")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("request survived")
	}
}
