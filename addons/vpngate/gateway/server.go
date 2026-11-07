package main

import (
	"context"
	"errors"
	"net"
	"sync"
	"syscall"
	"time"
)

type connectionTracker interface {
	track(net.Conn) bool
	untrack(net.Conn)
}

func retryableAcceptError(err error) bool {
	// Some platforms do not classify resource exhaustion as net.Error.Temporary.
	if errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE) || errors.Is(err, syscall.ENOBUFS) || errors.Is(err, syscall.ENOMEM) {
		return true
	}
	var temporary net.Error
	return errors.As(err, &temporary) && temporary.Temporary()
}

// Admission happens before spawning a handler. Shutdown or a permanent listener
// failure cancels all handlers and waits for their sockets and buffers to be released.
func serveConnections(parent context.Context, ln net.Listener, tracker connectionTracker, handle func(context.Context, net.Conn)) error {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(ctx, func() { ln.Close() })
	var handlers sync.WaitGroup
	defer func() {
		cancel()
		ln.Close()
		stop()
		handlers.Wait()
	}()
	var retryDelay time.Duration
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// Descriptor exhaustion can clear as existing sessions finish. Preserve
			// those sessions and back off instead of stopping the entire gateway.
			if retryableAcceptError(err) {
				retryDelay = min(max(2*retryDelay, 5*time.Millisecond), time.Second)
				timer := time.NewTimer(retryDelay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil
				case <-timer.C:
					continue
				}
			}
			return err
		}
		retryDelay = 0
		if ctx.Err() != nil {
			c.Close()
			return nil
		}
		if tracker != nil && !tracker.track(c) {
			c.Close()
			continue
		}
		handlers.Add(1)
		go func() {
			defer handlers.Done()
			stop := context.AfterFunc(ctx, func() { c.Close() })
			defer func() {
				stop()
				c.Close()
				if tracker != nil {
					tracker.untrack(c)
				}
			}()
			handle(ctx, c)
		}()
	}
}
