// SOCKS5 framing and UDP relay adapt fanout (MIT); see NOTICE.md.
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"time"
)

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
	return serveConnections(ctx, ln, access, func(ctx context.Context, c net.Conn) { handleSocks(ctx, c, access) })
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
	relay(ctx, c, remote)
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
	address := ip.To4()
	if address == nil || port < 0 || port > 65535 {
		return errors.New("invalid SOCKS reply address")
	}
	b := [10]byte{5, code, 0, 1}
	copy(b[4:8], address)
	binary.BigEndian.PutUint16(b[8:], uint16(port))
	n, err := w.Write(b[:])
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	return err
}
