// Package runtime adapts the pinned sing-box core. Authentication is performed
// by the protocol, then every stream is guarded before routing (including DNS
// hijack and multiplexed streams). There is no observation-only billing path.
package runtime

import (
	"context"
	"io"
	"net"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sck03/sing-box-vpn/internal/accounting"
)

type guardedRouter struct {
	adapter.Router
	meter *accounting.Meter
}

func (r *guardedRouter) RouteConnectionEx(ctx context.Context, c net.Conn, m adapter.InboundContext, onClose N.CloseHandlerFunc) {
	h, err := r.meter.Open(ctx, m.User, m.Inbound, c)
	if err != nil {
		c.Close()
		if onClose != nil {
			onClose(err)
		}
		return
	}
	r.Router.RouteConnectionEx(ctx, &stream{Conn: c, handle: h}, m, func(err error) {
		h.Close()
		if onClose != nil {
			onClose(err)
		}
	})
}
func (r *guardedRouter) RoutePacketConnectionEx(ctx context.Context, c N.PacketConn, m adapter.InboundContext, onClose N.CloseHandlerFunc) {
	h, err := r.meter.Open(ctx, m.User, m.Inbound, c)
	if err != nil {
		c.Close()
		if onClose != nil {
			onClose(err)
		}
		return
	}
	r.Router.RoutePacketConnectionEx(ctx, &packets{PacketConn: c, handle: h}, m, func(err error) {
		h.Close()
		if onClose != nil {
			onClose(err)
		}
	})
}
func (r *guardedRouter) RouteConnection(ctx context.Context, c net.Conn, m adapter.InboundContext) error {
	h, err := r.meter.Open(ctx, m.User, m.Inbound, c)
	if err != nil {
		c.Close()
		return err
	}
	return r.Router.RouteConnection(ctx, &stream{Conn: c, handle: h}, m)
}
func (r *guardedRouter) RoutePacketConnection(ctx context.Context, c N.PacketConn, m adapter.InboundContext) error {
	h, err := r.meter.Open(ctx, m.User, m.Inbound, c)
	if err != nil {
		c.Close()
		return err
	}
	return r.Router.RoutePacketConnection(ctx, &packets{PacketConn: c, handle: h}, m)
}

// Do not expose Upstream, ReaderFrom, WriterTo or replaceability interfaces:
// the core's zero-copy paths must not unwrap and bypass payload accounting.
type stream struct {
	net.Conn
	handle *accounting.Handle
	once   sync.Once
}

func (c *stream) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n == 0 {
		return n, err
	}
	// A short reservation is a pool boundary, not necessarily quota exhaustion.
	// Account for every byte already read before handing it to the router.
	for accepted := 0; accepted < n; {
		limit, denied := c.handle.Take(n - accepted)
		if denied != nil {
			c.Close()
			return accepted, denied
		}
		c.handle.Complete(limit, limit, true)
		accepted += limit
	}
	return n, err
}
func (c *stream) Write(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		limit, err := c.handle.Take(len(p) - written)
		if err != nil {
			c.Close()
			return written, err
		}
		n, err := c.Conn.Write(p[written : written+limit])
		c.handle.Complete(limit, n, false)
		written += n
		if err != nil {
			return written, err
		}
		if n != limit {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}
func (c *stream) Close() error      { c.once.Do(c.handle.Close); return c.Conn.Close() }
func (c *stream) CloseRead() error  { return N.CloseRead(c.Conn) }
func (c *stream) CloseWrite() error { return N.CloseWrite(c.Conn) }

type packets struct {
	N.PacketConn
	handle *accounting.Handle
	once   sync.Once
}

// Forward buffer sizing metadata without exposing an unwrap path. Protocols
// such as VMess/VLESS prepend framing bytes when writing a UDP response.
func (c *packets) FrontHeadroom() int  { return N.CalculateFrontHeadroom(c.PacketConn) }
func (c *packets) RearHeadroom() int   { return N.CalculateRearHeadroom(c.PacketConn) }
func (c *packets) ReaderOverhead() int { return N.CalculateReaderOverhead(c.PacketConn) }
func (c *packets) ReaderMTU() int      { return N.CalculateMTU(c.PacketConn, nil) }
func (c *packets) WriterMTU() int      { return N.CalculateMTU(nil, c.PacketConn) }

func (c *packets) ReadPacket(b *buf.Buffer) (M.Socksaddr, error) {
	destination, err := c.PacketConn.ReadPacket(b)
	if err != nil {
		return destination, err
	}
	n, err := c.handle.TakePacket(b.Len())
	if err != nil {
		c.Close()
		return destination, err
	}
	if n != b.Len() {
		c.handle.Complete(n, 0, true)
		c.Close()
		return destination, accounting.ErrDenied
	}
	c.handle.Complete(n, n, true)
	return destination, nil
}
func (c *packets) WritePacket(b *buf.Buffer, destination M.Socksaddr) error {
	n, err := c.handle.TakePacket(b.Len())
	if err != nil {
		b.Release()
		c.Close()
		return err
	}
	if n != b.Len() {
		c.handle.Complete(n, 0, false)
		b.Release()
		c.Close()
		return accounting.ErrDenied
	}
	err = c.PacketConn.WritePacket(b, destination)
	if err != nil {
		c.handle.Complete(n, 0, false)
	} else {
		c.handle.Complete(n, n, false)
	}
	return err
}
func (c *packets) Close() error { c.once.Do(c.handle.Close); return c.PacketConn.Close() }
