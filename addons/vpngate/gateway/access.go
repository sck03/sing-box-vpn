package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
)

type ticket struct {
	epoch   uint64
	country string
}

type proxyAccess interface {
	connectionTracker
	authenticate(string, string) (ticket, bool)
	allowed(ticket) bool
	open(context.Context, ticket, string, string) (net.Conn, string, error)
}

type pendingDial struct{ cancel context.CancelFunc }

var errConnectionLimit = errors.New("proxy connection limit reached")

func equalSecret(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// Capacity includes both accepted sockets and worker dials that have not replied.
// These helpers require m.mu; reservation and promotion must be one transaction.
func (m *manager) hasCapacity() bool {
	return len(m.connections)+len(m.pendingDials) < m.cfg.MaxConnections
}

func (m *manager) allowedLocked(t ticket) bool {
	return m.ready && t.epoch == m.epoch && t.country == m.cfg.Country
}

func (m *manager) revoke() {
	m.mu.Lock()
	m.ready = false
	m.epoch++
	connections, pending := m.connections, m.pendingDials
	m.connections = map[net.Conn]bool{}
	m.pendingDials = map[*pendingDial]struct{}{}
	m.mu.Unlock()
	for dial := range pending {
		dial.cancel()
	}
	for c := range connections {
		c.Close()
	}
}

func (m *manager) track(c net.Conn) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.hasCapacity() {
		return false
	}
	m.connections[c] = true
	return true
}

func (m *manager) untrack(c net.Conn) { m.mu.Lock(); delete(m.connections, c); m.mu.Unlock() }

func (m *manager) authenticate(user, pass string) (ticket, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return ticket{epoch: m.epoch, country: m.cfg.Country}, equalSecret(user, m.cfg.ProxyUser) && equalSecret(pass, m.cfg.ProxyPass)
}

func (m *manager) allowed(t ticket) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.allowedLocked(t)
}

func (m *manager) open(ctx context.Context, t ticket, network, address string) (net.Conn, string, error) {
	m.mu.Lock()
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		return nil, "", err
	}
	active := m.current
	if !m.allowedLocked(t) || active == nil {
		m.mu.Unlock()
		return nil, "", errors.New("verified tunnel is unavailable")
	}
	if !m.hasCapacity() {
		m.mu.Unlock()
		return nil, "", errConnectionLimit
	}
	ctx, cancel := context.WithCancel(ctx)
	pending := &pendingDial{cancel: cancel}
	m.pendingDials[pending] = struct{}{}
	m.mu.Unlock()
	defer cancel()

	c, target, err := dialWorker(ctx, active.socket(), network, address)
	m.mu.Lock()
	delete(m.pendingDials, pending)
	if err == nil {
		if ctx.Err() != nil || !m.allowedLocked(t) || m.current != active {
			err = errors.New("tunnel changed during dial")
		} else {
			// Convert the reservation without releasing its capacity to another caller.
			m.connections[c] = true
		}
	}
	m.mu.Unlock()
	if err != nil {
		if c != nil {
			c.Close()
		}
		return nil, "", err
	}
	return c, target, nil
}
