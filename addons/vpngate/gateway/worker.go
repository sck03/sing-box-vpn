package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

const maxPacket = 65507
const udpIdleTimeout = 60 * time.Second

type dialRequest struct {
	Network string `json:"network"`
	Address string `json:"address"`
}
type dialResponse struct {
	Address string `json:"address,omitempty"`
	Error   string `json:"error,omitempty"`
}

func writeFrame(w io.Writer, b []byte) error {
	if len(b) > 65535 {
		return errors.New("frame too large")
	}
	var h [2]byte
	binary.BigEndian.PutUint16(h[:], uint16(len(b)))
	if n, e := w.Write(h[:]); e != nil {
		return e
	} else if n != len(h) {
		return io.ErrShortWrite
	}
	n, e := w.Write(b)
	if e == nil && n != len(b) {
		return io.ErrShortWrite
	}
	return e
}

// UDP callers reserve two bytes in their existing packet buffer. Writing the
// header and payload together avoids a second syscall and per-packet allocation.
func writePacket(w io.Writer, packet []byte) error {
	if len(packet) < 2 || len(packet) > maxPacket+2 {
		return errors.New("invalid UDP frame size")
	}
	binary.BigEndian.PutUint16(packet[:2], uint16(len(packet)-2))
	n, err := w.Write(packet)
	if err == nil && n != len(packet) {
		err = io.ErrShortWrite
	}
	return err
}
func readFrame(r io.Reader, limit int) ([]byte, error) {
	var h [2]byte
	if _, e := io.ReadFull(r, h[:]); e != nil {
		return nil, e
	}
	n := int(binary.BigEndian.Uint16(h[:]))
	if n > limit {
		return nil, errors.New("frame limit exceeded")
	}
	b := make([]byte, n)
	_, e := io.ReadFull(r, b)
	return b, e
}

type frameReader struct {
	source        io.Reader
	header        [2]byte
	buffer        []byte
	limit, prefix int
}

// Retain only the observed packet capacity; prefix reserves a SOCKS header.
func (r *frameReader) read() ([]byte, error) {
	if _, e := io.ReadFull(r.source, r.header[:]); e != nil {
		return nil, e
	}
	n := int(binary.BigEndian.Uint16(r.header[:]))
	if n > r.limit {
		return nil, errors.New("frame limit exceeded")
	}
	size := r.prefix + n
	if size > cap(r.buffer) {
		capacity := min(r.prefix+r.limit, max(2048, 2*cap(r.buffer), size))
		buf := make([]byte, size, capacity)
		copy(buf, r.buffer[:r.prefix])
		r.buffer = buf
	} else {
		r.buffer = r.buffer[:size]
	}
	_, e := io.ReadFull(r.source, r.buffer[r.prefix:])
	return r.buffer, e
}

func writeObject(w io.Writer, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return writeFrame(w, b)
}

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
	targets := make([]string, 0, len(ips))
	for _, ip := range ips {
		if v := ip.To4(); v != nil && publicIPv4(v.String()) {
			targets = append(targets, net.JoinHostPort(v.String(), port))
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
		c, err := dial(attempt, network+"4", address)
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
		buf := make([]byte, maxPacket+2)
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

func relay(a, b net.Conn) {
	copyTo := func(dst, src net.Conn) {
		_, e := io.Copy(dst, src)
		if e == nil {
			if half, ok := dst.(interface{ CloseWrite() error }); ok {
				e = half.CloseWrite()
			} else {
				e = dst.Close()
			}
		}
		if e != nil {
			a.Close()
			b.Close()
		}
	}
	done := make(chan struct{}, 1)
	go func() { copyTo(a, b); done <- struct{}{} }()
	copyTo(b, a)
	<-done
	a.Close()
	b.Close()
}

func dialWorker(ctx context.Context, socket, network, address string) (net.Conn, string, error) {
	c, e := (&net.Dialer{Timeout: 20 * time.Second}).DialContext(ctx, "unix", socket)
	if e != nil {
		return nil, "", e
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	deadline := time.Now().Add(25 * time.Second)
	if limit, exists := ctx.Deadline(); exists && limit.Before(deadline) {
		deadline = limit
	}
	_ = c.SetDeadline(deadline)
	if e = writeObject(c, dialRequest{Network: network, Address: address}); e != nil {
		return nil, "", e
	}
	b, e := readFrame(c, 4096)
	if e != nil {
		return nil, "", e
	}
	var reply dialResponse
	if e = json.Unmarshal(b, &reply); e != nil {
		return nil, "", e
	}
	if reply.Error != "" {
		return nil, "", fmt.Errorf("tunnel dial: %s", reply.Error)
	}
	if strings.TrimSpace(reply.Address) == "" {
		return nil, "", errors.New("worker target missing")
	}
	if e = ctx.Err(); e != nil {
		return nil, "", e
	}
	_ = c.SetDeadline(time.Time{})
	ok = true
	return c, reply.Address, nil
}
