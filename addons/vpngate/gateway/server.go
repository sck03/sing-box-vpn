package main

import (
	"context"
	"net"
	"sync"
)

type connectionTracker interface {
	track(net.Conn) bool
	untrack(net.Conn)
}

// Admission happens before spawning a handler. Closing or failing the listener
// cancels all handlers and waits for their sockets and buffers to be released.
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
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
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
