package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"
)

type worker struct {
	socket       string
	dns          string
	resolver     *net.Resolver
	resolverOnce sync.Once
	dialOverride func(context.Context, string, string) (net.Conn, error)
}

func (w *worker) dial(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "udp" {
		return nil, errors.New("unsupported network")
	}
	if w.dialOverride != nil {
		return w.dialOverride(ctx, network, address)
	}
	host, port, e := net.SplitHostPort(address)
	if e != nil || !validPort(port) {
		return nil, errors.New("invalid target")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var ips []net.IP
	if literal := net.ParseIP(host); literal != nil {
		ips = []net.IP{literal}
	} else {
		w.resolverOnce.Do(func() {
			w.resolver = &net.Resolver{PreferGo: true, Dial: func(c context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(c, network+"4", w.dns)
			}}
		})
		ips, e = w.resolver.LookupIP(ctx, "ip4", host)
		if e != nil {
			return nil, e
		}
	}
	return dialPublicTargets(ctx, network, port, ips, (&net.Dialer{KeepAlive: 30 * time.Second}).DialContext)
}

func dialPublicTargets(ctx context.Context, network, port string, ips []net.IP, dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	targets := make([]netip.Addr, 0, len(ips))
	seen := make(map[netip.Addr]bool, len(ips))
	for _, ip := range ips {
		if address, ok := netip.AddrFromSlice(ip); ok {
			address = address.Unmap()
			if publicIPv4Addr(address) && !seen[address] {
				targets = append(targets, address)
				seen[address] = true
			}
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("only public IPv4 targets are permitted")
	}
	deadline := time.Now().Add(20 * time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	var firstErr error
	for i, address := range targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, context.DeadlineExceeded
		}
		// Share the overall budget, like net.Dialer's serial address fallback.
		budget := min(remaining, max(2*time.Second, remaining/time.Duration(len(targets)-i)))
		attempt, cancel := context.WithTimeout(ctx, budget)
		c, err := dial(attempt, network+"4", net.JoinHostPort(address.String(), port))
		cancel()
		if err == nil {
			return c, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

func (w *worker) serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ln, e := net.Listen("unix", w.socket)
	if e != nil {
		return e
	}
	defer ln.Close()
	defer os.Remove(w.socket)
	if e = os.Chmod(w.socket, 0600); e != nil {
		return e
	}
	var mu sync.Mutex
	conns := map[net.Conn]bool{}
	go func() {
		<-ctx.Done()
		ln.Close()
		mu.Lock()
		for c := range conns {
			c.Close()
		}
		mu.Unlock()
	}()
	for {
		c, e := ln.Accept()
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return e
		}
		mu.Lock()
		if ctx.Err() != nil {
			mu.Unlock()
			c.Close()
			return nil
		}
		conns[c] = true
		mu.Unlock()
		go func() { defer func() { c.Close(); mu.Lock(); delete(conns, c); mu.Unlock() }(); w.handle(ctx, c) }()
	}
}

func (w *worker) handle(parent context.Context, c net.Conn) {
	_ = c.SetDeadline(time.Now().Add(25 * time.Second))
	b, e := readFrame(c, 4096)
	if e != nil {
		return
	}
	var req dialRequest
	if e = json.Unmarshal(b, &req); e != nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	remote, e := w.dial(ctx, req.Network, req.Address)
	cancel()
	if e != nil {
		_ = writeObject(c, dialResponse{Error: e.Error()})
		return
	}
	defer remote.Close()
	stop := context.AfterFunc(parent, func() { c.Close(); remote.Close() })
	defer stop()
	if e = writeObject(c, dialResponse{Address: remote.RemoteAddr().String()}); e != nil {
		return
	}
	_ = c.SetDeadline(time.Time{})
	if req.Network == "tcp" {
		relay(c, remote)
		return
	}
	done := make(chan struct{}, 1)
	go func() {
		defer func() { done <- struct{}{}; c.Close() }()
		buf := packetBuffers.Get().(*packetBuffer)
		defer packetBuffers.Put(buf)
		for {
			_ = remote.SetReadDeadline(time.Now().Add(udpIdleTimeout))
			n, e := remote.Read(buf[2:])
			if e != nil {
				return
			}
			// Replies keep a download-only session alive while its request side waits.
			_ = c.SetDeadline(time.Now().Add(udpIdleTimeout))
			if writePacket(c, buf[:n+2]) != nil {
				return
			}
		}
	}()
	frames := frameReader{source: c, limit: maxPacket}
	for {
		_ = c.SetReadDeadline(time.Now().Add(udpIdleTimeout))
		b, e := frames.read()
		if e != nil {
			break
		}
		// Upload traffic also counts as activity when the target sends no replies.
		_ = remote.SetDeadline(time.Now().Add(udpIdleTimeout))
		if _, e = remote.Write(b); e != nil {
			break
		}
	}
	remote.Close()
	c.Close()
	<-done
}
