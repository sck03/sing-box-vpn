// Package httpserver owns bounded HTTP admission and server lifetime.
package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
)

// Gate limits active requests. Keep separate gates for streams and short work.
type Gate chan struct{}

func (g Gate) Enter(w http.ResponseWriter) (func(), bool) {
	release, ok := g.TryEnter()
	if !ok {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "service busy; retry shortly", http.StatusServiceUnavailable)
	}
	return release, ok
}

// TryEnter lets transports select their own busy response envelope.
func (g Gate) TryEnter() (func(), bool) {
	select {
	case g <- struct{}{}:
		return func() { <-g }, true
	default:
		return nil, false
	}
}

func (g Gate) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release, ok := g.Enter(w)
		if !ok {
			return
		}
		defer release()
		next.ServeHTTP(w, r)
	})
}

type listener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
}
type connection struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *connection) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
func (l *listener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &connection{Conn: c, release: func() { <-l.slots }}, nil
}
func (l *listener) Close() error {
	var err error
	l.once.Do(func() { close(l.done); err = l.Listener.Close() })
	return err
}

func Serve(ctx context.Context, socket net.Listener, server *http.Server, connections int) error {
	limited := &listener{Listener: socket, slots: make(chan struct{}, connections), done: make(chan struct{})}
	defer limited.Close()
	defer server.Close()
	stop := context.AfterFunc(ctx, func() { server.Close() })
	defer stop()
	var err error
	if server.TLSConfig != nil {
		// TLS must wrap the limited raw connection, so net/http receives a
		// *tls.Conn and retains request TLS state and HTTP/2 negotiation.
		err = server.ServeTLS(limited, "", "")
	} else {
		err = server.Serve(limited)
	}
	if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return err
}
