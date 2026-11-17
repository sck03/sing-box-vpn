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
	select {
	case g <- struct{}{}:
		return func() { <-g }, true
	default:
		w.Header().Set("Retry-After", "5")
		http.Error(w, "service busy; retry shortly", http.StatusServiceUnavailable)
		return nil, false
	}
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
	err := server.Serve(limited)
	if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return err
}
