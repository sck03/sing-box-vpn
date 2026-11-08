package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

type packetBuffer [maxPacket + 2]byte

// UDP receives need a full datagram buffer; reuse it across completed sessions.
var packetBuffers = sync.Pool{New: func() any { return new(packetBuffer) }}

// EOF half-closes only the destination so the reply can finish. Cancellation
// must close both ends even when one copy has already completed successfully.
func relay(ctx context.Context, a, b net.Conn) {
	closeConnections := func() { a.Close(); b.Close() }
	stop := context.AfterFunc(ctx, closeConnections)
	defer stop()
	defer closeConnections()
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
			closeConnections()
		}
	}
	done := make(chan struct{})
	go func() { defer close(done); copyTo(a, b) }()
	copyTo(b, a)
	<-done
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
