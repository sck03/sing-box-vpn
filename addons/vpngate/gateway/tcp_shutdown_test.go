package main

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// Keep the worker's response stream open after the client's request EOF. Its
// lifetime is deliberately independent of the proxy so it cannot mask a leak.
func TestProxyShutdownClosesHalfClosedRelay(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "cancel"
		if failure {
			name = "listener-failure"
		}
		t.Run(name, func(t *testing.T) {
			workerListener, err := net.Listen("unix", filepath.Join(t.TempDir(), "worker.sock"))
			if err != nil {
				t.Fatal(err)
			}
			defer workerListener.Close()
			requestEOF := make(chan error, 1)
			release := make(chan struct{})
			workerDone := make(chan struct{})
			defer func() { close(release); workerListener.Close(); <-workerDone }()
			go func() {
				defer close(workerDone)
				c, err := workerListener.Accept()
				if err != nil {
					requestEOF <- err
					return
				}
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				if _, err = readFrame(c, 4096); err == nil {
					err = writeObject(c, dialResponse{Address: "203.0.113.5:80"})
				}
				if err == nil {
					_, err = io.Copy(io.Discard, c)
				}
				requestEOF <- err
				<-release
			}()

			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			access := &testAccess{ready: true, socket: workerListener.Addr().String(), conns: map[net.Conn]bool{}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { defer close(done); done <- serveProxy(ctx, listener, access) }()
			t.Cleanup(func() { cancel(); listener.Close(); <-done })
			client := authProxy(t, listener.Addr().String(), "password")
			defer client.Close()
			if _, err = client.Write(requestAddress(1, "203.0.113.5:80")); err != nil {
				t.Fatal(err)
			}
			var reply [10]byte
			if _, err = io.ReadFull(client, reply[:]); err != nil || reply[1] != 0 {
				t.Fatal("CONNECT failed", reply, err)
			}
			if err = client.(*net.TCPConn).CloseWrite(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-requestEOF:
				if err != nil {
					t.Fatal("request EOF was not forwarded", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("worker did not receive request EOF")
			}
			if failure {
				listener.Close()
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if (err != nil) != failure {
					t.Fatalf("proxy shutdown error: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("proxy shutdown retained a half-closed relay")
			}
			access.mu.Lock()
			remaining := len(access.conns)
			access.mu.Unlock()
			if remaining != 0 {
				t.Fatalf("stopped proxy retained %d connections", remaining)
			}
		})
	}
}
