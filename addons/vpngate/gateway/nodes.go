package main

import (
	"context"
	"maps"
	"slices"
	"time"
)

func (m *manager) refresh(ctx context.Context, c Config, force bool) []Node {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	m.mu.Lock()
	epoch := m.sourceEpoch
	m.mu.Unlock()
	started := time.Now()
	select {
	case m.refreshSlot <- struct{}{}:
		defer func() { <-m.refreshSlot }()
	case <-ctx.Done():
		return nil
	}
	m.mu.Lock()
	if ctx.Err() != nil || epoch != m.sourceEpoch || !slices.Equal(c.Sources, m.cfg.Sources) {
		m.mu.Unlock()
		return nil
	}
	nodes := m.nodes // Published node slices are immutable.
	fresh := len(nodes) > 0 && time.Since(m.nodesUpdated) < time.Minute && (!force || m.nodesUpdated.After(started))
	m.mu.Unlock()
	if fresh {
		return nodes
	}
	for _, source := range c.Sources {
		if ctx.Err() != nil {
			return nil
		}
		nodes, e := readNodeSource(ctx, m.sourceClient, source)
		if e != nil {
			continue
		}
		m.mu.Lock()
		if ctx.Err() != nil || epoch != m.sourceEpoch || !slices.Equal(c.Sources, m.cfg.Sources) {
			m.mu.Unlock()
			return nil
		}
		m.nodes = nodes
		m.nodesUpdated = time.Now()
		m.mu.Unlock()
		_ = saveSourceCache(c, "nodes.json", nodes)
		return nodes
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil || epoch != m.sourceEpoch || !slices.Equal(c.Sources, m.cfg.Sources) {
		return nil
	}
	nodes = m.nodes
	return nodes
}

func (m *manager) scheduleRefresh(parent context.Context, c Config, force bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refreshStopped || m.refreshDone != nil || parent.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	m.refreshDone, m.refreshCancel = done, cancel
	go func() {
		defer func() {
			cancel()
			m.mu.Lock()
			m.refreshDone, m.refreshCancel = nil, nil
			close(done)
			m.mu.Unlock()
		}()
		m.refresh(ctx, c, force)
	}()
}

// Stop accepting refresh requests before waiting; a late control handler cannot
// start new work after shutdown has captured the current background operation.
func (m *manager) stopRefresh() {
	m.mu.Lock()
	m.refreshStopped = true
	done, cancel := m.refreshDone, m.refreshCancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func candidates(c Config, nodes []Node, previous Node, swap bool, failed map[string]time.Time) []Node {
	var pool []Node
	seen := map[string]bool{}
	accept := func(n Node) bool {
		if c.Mode != "fixed" {
			if _, blocked := failed[n.Host]; blocked {
				return false
			}
		}
		return n.Country == c.Country && countryPattern.MatchString(n.Country) && publicIPv4(n.IP) && (!c.ResidentialOnly || volunteer(n)) && (!swap || n.Host != previous.Host)
	}
	add := func(n Node) {
		if !accept(n) || seen[n.Host] {
			return
		}
		profile, port, proto, e := validateProfile(n.Profile, n.IP)
		if e != nil {
			return
		}
		n.Profile = profile
		n.Port = port
		n.Network = proto
		pool = append(pool, n)
		seen[n.Host] = true
	}
	if c.Mode == "fixed" {
		host := c.FixedHost
		if host == "" {
			host = previous.Host
		}
		for _, n := range nodes {
			if host != "" && n.Host == host {
				add(n)
				if len(pool) > 0 {
					return pool
				}
			}
		}
		if host != "" && previous.Host == host {
			add(previous)
		}
		if len(pool) > 0 {
			return pool
		}
		if c.FixedHost != "" {
			return nil
		}
	}
	if !swap {
		for _, n := range nodes {
			if n.Host == previous.Host {
				add(n)
				break
			}
		}
		if len(pool) == 0 {
			add(previous)
		}
	}
	limit := 6
	if c.Mode == "fixed" {
		limit = 1
	}
	for _, n := range nodes {
		add(n)
		if len(pool) >= limit {
			break
		}
	}
	return pool
}

func (m *manager) reconnectCandidates(c Config, nodes []Node, swap bool) []Node {
	m.mu.Lock()
	if !slices.Equal(c.Sources, m.cfg.Sources) {
		m.mu.Unlock()
		return nil
	}
	now := time.Now()
	for host, until := range m.failed {
		if !now.Before(until) {
			delete(m.failed, host)
		}
	}
	previous, failed := m.previous, maps.Clone(m.failed)
	m.mu.Unlock()
	// Profile parsing can be expensive; admission and revocation need this lock too.
	return candidates(c, nodes, previous, swap, failed)
}
