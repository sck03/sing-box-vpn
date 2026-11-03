// SOCKS5 framing and UDP relay adapt fanout (MIT); see NOTICE.md.
package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

type ticket struct {
	epoch   uint64
	country string
}
type proxyAccess interface {
	authenticate(string, string) (ticket, bool)
	allowed(ticket) bool
	open(context.Context, ticket, string, string) (net.Conn, string, error)
	track(net.Conn) bool
	untrack(net.Conn)
}

func equalSecret(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func readSmallString(r io.Reader) (string, error) {
	var n [1]byte
	if _, e := io.ReadFull(r, n[:]); e != nil {
		return "", e
	}
	if n[0] == 0 {
		return "", errors.New("empty credential")
	}
	b := make([]byte, int(n[0]))
	_, e := io.ReadFull(r, b)
	return string(b), e
}

func serveProxy(ctx context.Context, ln net.Listener, access proxyAccess) error {
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		c, e := ln.Accept()
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return e
		}
		if !access.track(c) {
			c.Close()
			continue
		}
		go func() { defer c.Close(); defer access.untrack(c); handleSocks(ctx, c, access) }()
	}
}

func handleSocks(ctx context.Context, c net.Conn, a proxyAccess) {
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	var h [2]byte
	if _, e := io.ReadFull(c, h[:]); e != nil || h[0] != 5 || h[1] == 0 {
		return
	}
	methods := make([]byte, int(h[1]))
	if _, e := io.ReadFull(c, methods); e != nil {
		return
	}
	hasAuth := false
	for _, m := range methods {
		if m == 2 {
			hasAuth = true
		}
	}
	if !hasAuth {
		_, _ = c.Write([]byte{5, 255})
		return
	}
	if _, e := c.Write([]byte{5, 2}); e != nil {
		return
	}
	var version [1]byte
	if _, e := io.ReadFull(c, version[:]); e != nil || version[0] != 1 {
		return
	}
	user, e := readSmallString(c)
	if e != nil {
		return
	}
	pass, e := readSmallString(c)
	if e != nil {
		return
	}
	t, ok := a.authenticate(user, pass)
	if !ok {
		_, _ = c.Write([]byte{1, 1})
		return
	}
	if _, e = c.Write([]byte{1, 0}); e != nil {
		return
	}
	var request [4]byte
	if _, e = io.ReadFull(c, request[:]); e != nil || request[0] != 5 || request[2] != 0 {
		return
	}
	if request[1] != 1 && request[1] != 3 {
		socksReply(c, 7, net.IPv4zero, 0)
		return
	}
	var address string
	if request[1] == 3 && request[3] == 4 {
		// Association bind information is not a proxied IPv6 destination.
		bind := make([]byte, 18)
		_, e = io.ReadFull(c, bind)
		if e == nil && !net.IP(bind[:16]).IsUnspecified() && !net.IP(bind[:16]).IsLoopback() {
			e = errors.New("invalid UDP association bind")
		}
	} else {
		address, e = readSocksAddress(c, request[3])
	}
	if e != nil {
		socksReply(c, 8, net.IPv4zero, 0)
		return
	}
	if !a.allowed(t) {
		socksReply(c, 1, net.IPv4zero, 0)
		return
	}
	if request[1] == 3 {
		_ = c.SetDeadline(time.Time{})
		serveUDP(ctx, c, t, a)
		return
	}
	remote, _, e := a.open(ctx, t, "tcp", address)
	if e != nil {
		socksReply(c, 4, net.IPv4zero, 0)
		return
	}
	defer remote.Close()
	defer a.untrack(remote)
	if e = socksReply(c, 0, net.IPv4(127, 0, 0, 1), 0); e != nil {
		return
	}
	_ = c.SetDeadline(time.Time{})
	relay(c, remote)
}

func readSocksAddress(r io.Reader, kind byte) (string, error) {
	var host string
	switch kind {
	case 1:
		b := make([]byte, 4)
		if _, e := io.ReadFull(r, b); e != nil {
			return "", e
		}
		host = net.IP(b).String()
	case 3:
		var n [1]byte
		if _, e := io.ReadFull(r, n[:]); e != nil || n[0] == 0 {
			return "", errors.New("invalid domain")
		}
		b := make([]byte, int(n[0]))
		if _, e := io.ReadFull(r, b); e != nil {
			return "", e
		}
		host = string(b)
	default:
		return "", errors.New("IPv6 targets are unsupported")
	}
	var p [2]byte
	if _, e := io.ReadFull(r, p[:]); e != nil {
		return "", e
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(p[:])))), nil
}
func socksReply(w io.Writer, code byte, ip net.IP, port int) error {
	b := []byte{5, code, 0, 1}
	b = append(b, ip.To4()...)
	b = binary.BigEndian.AppendUint16(b, uint16(port))
	_, e := w.Write(b)
	return e
}

type udpSession struct {
	c      net.Conn
	target string
	client netip.AddrPort
}

func serveUDP(ctx context.Context, control net.Conn, t ticket, a proxyAccess) {
	tcpAddr, ok := control.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return
	}
	clientIP, ok := netip.AddrFromSlice(tcpAddr.IP)
	if !ok {
		return
	}
	clientIP = clientIP.Unmap()
	sock, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if e != nil {
		socksReply(control, 1, net.IPv4zero, 0)
		return
	}
	if !a.track(sock) {
		sock.Close()
		return
	}
	defer sock.Close()
	defer a.untrack(sock)
	if e = socksReply(control, 0, net.IPv4(127, 0, 0, 1), sock.LocalAddr().(*net.UDPAddr).Port); e != nil {
		return
	}
	var mu sync.Mutex
	sessions := map[string]*udpSession{}
	var clientAddr netip.AddrPort
	defer func() {
		mu.Lock()
		for _, s := range sessions {
			s.c.Close()
			a.untrack(s.c)
		}
		mu.Unlock()
	}()
	go func() { _, _ = control.Read(make([]byte, 1)); sock.Close() }()
	buf := make([]byte, maxPacket)
	for {
		_ = sock.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, src, e := sock.ReadFromUDPAddrPort(buf)
		if e != nil {
			return
		}
		if src.Addr() != clientIP || !a.allowed(t) || n < 7 || buf[0] != 0 || buf[1] != 0 || buf[2] != 0 {
			continue
		}
		if clientAddr.IsValid() && src != clientAddr {
			continue
		}
		target, offset, e := udpTarget(buf[:n])
		if e != nil {
			continue
		}
		if !clientAddr.IsValid() {
			clientAddr = src
		}
		mu.Lock()
		s := sessions[target]
		mu.Unlock()
		if s == nil {
			mu.Lock()
			full := len(sessions) >= 128
			mu.Unlock()
			if full {
				continue
			}
			c, resolved, e := a.open(ctx, t, "udp", target)
			if e != nil {
				continue
			}
			s = &udpSession{c: c, target: resolved, client: src}
			mu.Lock()
			sessions[target] = s
			mu.Unlock()
			go func(key string, session *udpSession) {
				defer session.c.Close()
				defer a.untrack(session.c)
				defer func() { mu.Lock(); delete(sessions, key); mu.Unlock() }()
				h, e := udpResponseHeader(session.target)
				if e != nil {
					return
				}
				frames := frameReader{source: session.c, buffer: h, prefix: len(h), limit: maxPacket - len(h)}
				for {
					_ = session.c.SetReadDeadline(time.Now().Add(60 * time.Second))
					packet, e := frames.read()
					if e != nil {
						return
					}
					if _, e = sock.WriteToUDPAddrPort(packet, session.client); e != nil {
						return
					}
				}
			}(target, s)
		}
		if e = writeFrame(s.c, buf[offset:n]); e != nil {
			s.c.Close()
		}
	}
}

func udpTarget(b []byte) (string, int, error) {
	if len(b) < 4 {
		return "", 0, io.ErrUnexpectedEOF
	}
	r := bytes.NewReader(b[4:])
	address, e := readSocksAddress(r, b[3])
	return address, len(b) - r.Len(), e
}
func udpResponseHeader(target string) ([]byte, error) {
	h, p, e := net.SplitHostPort(target)
	if e != nil {
		return nil, e
	}
	ip := net.ParseIP(h).To4()
	if ip == nil {
		return nil, errors.New("worker returned non-IPv4 target")
	}
	port, e := strconv.Atoi(p)
	if e != nil || port < 1 || port > 65535 {
		return nil, errors.New("worker returned invalid UDP port")
	}
	b := append([]byte{0, 0, 0, 1}, ip...)
	return binary.BigEndian.AppendUint16(b, uint16(port)), nil
}
