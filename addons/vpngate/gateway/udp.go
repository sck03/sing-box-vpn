// SOCKS5 UDP relay adapts fanout (MIT); see NOTICE.md.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

const (
	maxUDPTargets       = 128
	maxUDPHeaders       = 256
	maxUDPDials         = 8
	maxUDPQueuedPackets = 64
	maxUDPQueuedBytes   = 256 << 10
)

type udpSession struct {
	target string
	c      net.Conn // Published once, under the association lock.
	queued [][]byte
	sendMu sync.Mutex
}

type udpAssociation struct {
	ctx         context.Context
	access      proxyAccess
	ticket      ticket
	socket      *net.UDPConn
	mu          sync.Mutex
	sessions    map[string]*udpSession
	headers     map[string]*udpSession
	pending     int
	queuedBytes int
	closed      sync.Once
	workers     sync.WaitGroup
}

func (u *udpAssociation) close() {
	u.closed.Do(func() {
		u.socket.Close()
		u.mu.Lock()
		defer u.mu.Unlock()
		for _, s := range u.sessions {
			if s.c != nil {
				s.c.Close()
			}
		}
	})
}

// Pending targets have bounded queues. Established targets write from the receive
// buffer directly, retaining the allocation-free path and their source ports.
func (u *udpAssociation) dispatch(packet []byte, offset int, client netip.AddrPort) {
	u.mu.Lock()
	if u.ctx.Err() != nil {
		u.mu.Unlock()
		return
	}
	s := u.headers[string(packet[3:offset])]
	if s == nil {
		target, _, err := udpTarget(packet)
		if err != nil {
			u.mu.Unlock()
			return
		}
		s = u.sessions[target]
		if s == nil {
			if len(u.sessions) >= maxUDPTargets || u.pending >= maxUDPDials {
				u.mu.Unlock()
				return
			}
			s = &udpSession{target: target}
			if !u.queue(s, packet[offset-2:]) {
				u.mu.Unlock()
				return
			}
			u.sessions[target] = s
			u.pending++
			if len(u.headers) < maxUDPHeaders {
				u.headers[string(packet[3:offset])] = s
			}
			u.workers.Add(1)
			u.mu.Unlock()
			go u.run(s, client)
			return
		}
		if len(u.headers) < maxUDPHeaders {
			u.headers[string(packet[3:offset])] = s
		}
	}
	c := s.c
	if c == nil {
		u.queue(s, packet[offset-2:])
	}
	u.mu.Unlock()
	if c != nil {
		s.sendMu.Lock()
		_ = sendUDPPacket(c, packet[offset-2:])
		s.sendMu.Unlock()
	}
}

// queue and takeQueued are called only while holding the association lock.
func (u *udpAssociation) queue(s *udpSession, packet []byte) bool {
	if len(s.queued) >= maxUDPQueuedPackets || u.queuedBytes+len(packet) > maxUDPQueuedBytes {
		return false
	}
	s.queued = append(s.queued, bytes.Clone(packet))
	u.queuedBytes += len(packet)
	return true
}

func (u *udpAssociation) takeQueued(s *udpSession) [][]byte {
	packets := s.queued
	for _, packet := range packets {
		u.queuedBytes -= len(packet)
	}
	s.queued = nil
	return packets
}

func (u *udpAssociation) remove(s *udpSession) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if s.c == nil {
		u.pending--
	}
	u.takeQueued(s)
	delete(u.sessions, s.target)
	for header, cached := range u.headers {
		if cached == s {
			delete(u.headers, header)
		}
	}
}

func sendUDPPacket(c net.Conn, packet []byte) error {
	// Both read and write activity extend the lifetime of one-way traffic.
	_ = c.SetDeadline(time.Now().Add(udpIdleTimeout))
	err := writePacket(c, packet)
	if err != nil {
		c.Close()
	}
	return err
}

func (u *udpAssociation) activate(s *udpSession, c net.Conn) error {
	// Publish the connection while holding its write lock, so queued datagrams
	// finish before new packets can write to the same framed stream.
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	u.mu.Lock()
	if err := u.ctx.Err(); err != nil {
		u.mu.Unlock()
		return err
	}
	s.c = c
	u.pending--
	packets := u.takeQueued(s)
	u.mu.Unlock()
	for _, packet := range packets {
		if err := sendUDPPacket(c, packet); err != nil {
			return err
		}
	}
	return nil
}

func (u *udpAssociation) run(s *udpSession, client netip.AddrPort) {
	defer u.workers.Done()
	defer u.remove(s)
	c, resolved, err := u.access.open(u.ctx, u.ticket, "udp", s.target)
	if err != nil {
		return
	}
	defer c.Close()
	defer u.access.untrack(c)
	header, err := udpResponseHeader(resolved)
	if err != nil {
		return
	}
	if err := u.activate(s, c); err != nil {
		return
	}
	frames := frameReader{source: c, buffer: header, prefix: len(header), limit: maxPacket - len(header)}
	for {
		_ = c.SetReadDeadline(time.Now().Add(udpIdleTimeout))
		packet, err := frames.read()
		if err != nil {
			return
		}
		_ = u.socket.SetDeadline(time.Now().Add(udpIdleTimeout))
		if _, err = u.socket.WriteToUDPAddrPort(packet, client); err != nil {
			return
		}
	}
}

func serveUDP(parent context.Context, control net.Conn, t ticket, access proxyAccess) {
	address, ok := control.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return
	}
	clientIP, ok := netip.AddrFromSlice(address.IP)
	if !ok {
		return
	}
	clientIP = clientIP.Unmap()
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		socksReply(control, 1, net.IPv4zero, 0)
		return
	}
	defer socket.Close()
	if !access.track(socket) {
		return
	}
	defer access.untrack(socket)
	if err := socksReply(control, 0, net.IPv4(127, 0, 0, 1), socket.LocalAddr().(*net.UDPAddr).Port); err != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	u := &udpAssociation{ctx: ctx, access: access, ticket: t, socket: socket, sessions: map[string]*udpSession{}, headers: map[string]*udpSession{}}
	closeAssociation := func() { control.Close(); u.close() }
	stop := context.AfterFunc(ctx, closeAssociation)
	defer func() {
		cancel()
		stop()
		closeAssociation()
		u.workers.Wait()
	}()
	go func() { var b [1]byte; _, _ = control.Read(b[:]); cancel() }()
	buffer := packetBuffers.Get().(*packetBuffer)
	defer packetBuffers.Put(buffer)
	var clientAddr netip.AddrPort
	for {
		_ = socket.SetReadDeadline(time.Now().Add(udpIdleTimeout))
		n, source, err := socket.ReadFromUDPAddrPort(buffer[:maxPacket])
		if err != nil {
			return
		}
		packet := buffer[:n]
		if source.Addr() != clientIP || !access.allowed(t) || n < 7 || packet[0] != 0 || packet[1] != 0 || packet[2] != 0 {
			continue
		}
		if clientAddr.IsValid() && source != clientAddr {
			continue
		}
		offset, err := udpHeaderSize(packet)
		if err != nil {
			continue
		}
		if !clientAddr.IsValid() {
			clientAddr = source
		}
		u.dispatch(packet, offset, source)
	}
}

func udpHeaderSize(b []byte) (int, error) {
	if len(b) < 4 {
		return 0, io.ErrUnexpectedEOF
	}
	size := 10
	switch b[3] {
	case 1:
	case 3:
		if len(b) < 5 || b[4] == 0 {
			return 0, errors.New("invalid domain")
		}
		size = 7 + int(b[4])
	default:
		return 0, errors.New("IPv6 targets are unsupported")
	}
	if len(b) < size {
		return 0, io.ErrUnexpectedEOF
	}
	return size, nil
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
