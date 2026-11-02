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
	if _, e := w.Write(h[:]); e != nil {
		return e
	}
	_, e := w.Write(b)
	return e
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
	if e != nil || !positiveInt(port) {
		return nil, errors.New("invalid target")
	}
	var ip net.IP
	if literal := net.ParseIP(host); literal != nil {
		ip = literal.To4()
	} else {
		resolver := net.Resolver{PreferGo: true, Dial: func(c context.Context, _ string, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(c, "udp4", w.dns)
		}}
		ips, e := resolver.LookupIP(ctx, "ip4", host)
		if e != nil {
			return nil, e
		}
		for _, v := range ips {
			if publicIPv4(v.String()) {
				ip = v.To4()
				break
			}
		}
	}
	if ip == nil || !publicIPv4(ip.String()) {
		return nil, errors.New("only public IPv4 targets are permitted")
	}
	return (&net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network+"4", net.JoinHostPort(ip.String(), port))
}

func (w *worker) serve(ctx context.Context) error {
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
		buf := make([]byte, maxPacket)
		for {
			_ = remote.SetReadDeadline(time.Now().Add(60 * time.Second))
			n, e := remote.Read(buf)
			if e != nil {
				return
			}
			if writeFrame(c, buf[:n]) != nil {
				return
			}
		}
	}()
	for {
		_ = c.SetReadDeadline(time.Now().Add(60 * time.Second))
		b, e := readFrame(c, maxPacket)
		if e != nil {
			break
		}
		if _, e = remote.Write(b); e != nil {
			break
		}
	}
	remote.Close()
	c.Close()
	<-done
}

func relay(a, b net.Conn) {
	done := make(chan struct{}, 1)
	go func() { _, _ = io.Copy(a, b); a.Close(); b.Close(); done <- struct{}{} }()
	_, _ = io.Copy(b, a)
	a.Close()
	b.Close()
	<-done
}

func dialWorker(ctx context.Context, socket, network, address string) (net.Conn, string, error) {
	c, e := (&net.Dialer{Timeout: 20 * time.Second}).DialContext(ctx, "unix", socket)
	if e != nil {
		return nil, "", e
	}
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	_ = c.SetDeadline(time.Now().Add(25 * time.Second))
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
	_ = c.SetDeadline(time.Time{})
	ok = true
	return c, reply.Address, nil
}
