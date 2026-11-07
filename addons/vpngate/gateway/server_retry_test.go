package main

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

type acceptResult struct {
	conn net.Conn
	err  error
}

type retryListener struct {
	results chan acceptResult
	calls   chan time.Time
	done    chan struct{}
	once    sync.Once
}

func newRetryListener() *retryListener {
	return &retryListener{results: make(chan acceptResult), calls: make(chan time.Time, 1), done: make(chan struct{})}
}

func (l *retryListener) Accept() (net.Conn, error) {
	l.calls <- time.Now()
	select {
	case result := <-l.results:
		return result.conn, result.err
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *retryListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (*retryListener) Addr() net.Addr { return &net.TCPAddr{} }

func acceptResourceError(errno syscall.Errno) error {
	return &net.OpError{Op: "accept", Net: "tcp", Err: os.NewSyscallError("accept", errno)}
}

func TestAcceptTemporaryErrorPreservesSessions(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.EMFILE, syscall.ENFILE, syscall.ENOBUFS, syscall.ENOMEM} {
		t.Run(errno.Error(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				ln := newRetryListener()
				done := make(chan error, 1)
				go func() {
					done <- serveConnections(ctx, ln, nil, func(_ context.Context, c net.Conn) { _, _ = io.Copy(c, c) })
				}()
				<-ln.calls
				server, client := net.Pipe()
				defer client.Close()
				ln.results <- acceptResult{conn: server}
				<-ln.calls
				ln.results <- acceptResult{err: acceptResourceError(errno)}
				synctest.Wait()
				select {
				case err := <-done:
					t.Fatalf("temporary accept error stopped active sessions: %v", err)
				default:
				}
				if _, err := client.Write([]byte("alive")); err != nil {
					t.Fatal(err)
				}
				var reply [5]byte
				if _, err := io.ReadFull(client, reply[:]); err != nil || string(reply[:]) != "alive" {
					t.Fatalf("existing session failed during retry: %q, %v", reply, err)
				}
				time.Sleep(5 * time.Millisecond)
				<-ln.calls
				cancel()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestAcceptRetryBackoffAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ln := newRetryListener()
		done := make(chan error, 1)
		go func() { done <- serveConnections(ctx, ln, nil, func(context.Context, net.Conn) {}) }()
		previous := <-ln.calls
		for i := range 11 {
			ln.results <- acceptResult{err: acceptResourceError(syscall.ENFILE)}
			synctest.Wait()
			select {
			case err := <-done:
				t.Fatalf("temporary error was not retried: %v", err)
			default:
			}
			delay := min(5*time.Millisecond<<i, time.Second)
			time.Sleep(delay - time.Nanosecond)
			select {
			case <-ln.calls:
				t.Fatal("accept retried without its backoff")
			default:
			}
			time.Sleep(time.Nanosecond)
			current := <-ln.calls
			if current.Sub(previous) != delay {
				t.Fatalf("retry delay = %s, want %s", current.Sub(previous), delay)
			}
			previous = current
		}
		// A successful accept resets the delay, even if the handler exits at once.
		server, client := net.Pipe()
		defer client.Close()
		ln.results <- acceptResult{conn: server}
		<-ln.calls
		ln.results <- acceptResult{err: acceptResourceError(syscall.EMFILE)}
		time.Sleep(5 * time.Millisecond)
		if current := <-ln.calls; current.Sub(previous) != 5*time.Millisecond {
			t.Fatalf("successful accept did not reset retry delay: %s", current.Sub(previous))
		}
		// Shutdown interrupts the wait instead of delaying listener/handler cleanup.
		ln.results <- acceptResult{err: acceptResourceError(syscall.EMFILE)}
		synctest.Wait()
		before := time.Now()
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if !time.Now().Equal(before) {
			t.Fatal("shutdown waited for retry backoff")
		}
	})
}

func TestAcceptPermanentErrorReturned(t *testing.T) {
	ln := newRetryListener()
	done := make(chan error, 1)
	go func() { done <- serveConnections(context.Background(), ln, nil, func(context.Context, net.Conn) {}) }()
	<-ln.calls
	want := errors.New("listener failed")
	ln.results <- acceptResult{err: want}
	if err := <-done; !errors.Is(err, want) {
		t.Fatalf("permanent accept error = %v, want %v", err, want)
	}
}
