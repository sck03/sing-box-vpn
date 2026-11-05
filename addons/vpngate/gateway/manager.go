package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

type tunnel interface {
	start(context.Context, Node) error
	stop()
	socket() string
	died() <-chan error
}

func (b *backend) socket() string     { return b.workerSocket }
func (b *backend) died() <-chan error { return b.exited }

type manager struct {
	mu             sync.Mutex
	actionsMu      sync.Mutex
	cfg            Config
	configPath     string
	status         Status
	ready          bool
	epoch          uint64
	current        tunnel
	previous       Node
	nodes          []Node
	nodesUpdated   time.Time
	refreshSlot    chan struct{}
	refreshPending bool
	sourceClient   *http.Client
	hostClient     *http.Client
	connections    map[net.Conn]bool
	wake           chan struct{}
	cancel         context.CancelFunc
	swap           bool
	factory        func(Config) tunnel
	probeOverride  func(context.Context, Config, tunnel) (Geo, error)
	geoCache       map[string]cachedGeo
	failed         map[string]time.Time
}
type cachedGeo struct {
	value   Geo
	expires time.Time
}

type probeUnavailable string

func (e probeUnavailable) Error() string { return string(e) }

func newManager(c Config, path string) *manager {
	m := &manager{cfg: c, configPath: path, status: Status{Version: version, State: "stopped", Country: c.Country, Mode: c.Mode, Updated: time.Now()}, connections: map[net.Conn]bool{}, wake: make(chan struct{}, 1), factory: func(c Config) tunnel { return &backend{cfg: c} }}
	m.refreshSlot = make(chan struct{}, 1)
	m.sourceClient = directClient(25 * time.Second)
	m.hostClient = &http.Client{Timeout: 12 * time.Second, Transport: m.sourceClient.Transport, CheckRedirect: m.sourceClient.CheckRedirect}
	m.geoCache = map[string]cachedGeo{}
	m.failed = map[string]time.Time{}
	if b, e := os.ReadFile(filepath.Join(c.DataDir, "last-node.json")); e == nil {
		_ = json.Unmarshal(b, &m.previous)
	}
	if b, e := os.ReadFile(filepath.Join(c.DataDir, "nodes.json")); e == nil {
		_ = json.Unmarshal(b, &m.nodes)
	}
	return m
}
func (m *manager) snapshot() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status
	if s.State == "ready" && !m.ready {
		s.State, s.Reason, s.Exit = "waiting", "forwarding blocked; waiting for exit verification", nil
	}
	if s.Exit != nil {
		g := *s.Exit
		s.Exit = &g
	}
	return s
}
func (m *manager) setStatus(state, reason string, node Node, geo *Geo) {
	m.mu.Lock()
	previous := m.status
	m.status = Status{Version: version, State: state, Reason: reason, Country: m.cfg.Country, Mode: m.cfg.Mode, Node: node.Host, Exit: geo, Updated: time.Now()}
	s := m.status
	sameExit := previous.Exit == nil && geo == nil || previous.Exit != nil && geo != nil && *previous.Exit == *geo
	changed := previous.State != s.State || previous.Reason != s.Reason || previous.Country != s.Country || previous.Mode != s.Mode || previous.Node != s.Node || !sameExit
	dir := m.cfg.DataDir
	m.mu.Unlock()
	if changed {
		_ = atomicJSON(filepath.Join(dir, "state.json"), s)
	}
}
func (m *manager) revoke() {
	m.mu.Lock()
	m.ready = false
	m.epoch++
	all := m.connections
	m.connections = map[net.Conn]bool{}
	m.mu.Unlock()
	for c := range all {
		c.Close()
	}
}
func (m *manager) track(c net.Conn) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.connections) >= m.cfg.MaxConnections {
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
	return m.ready && t.epoch == m.epoch && t.country == m.cfg.Country
}
func (m *manager) open(ctx context.Context, t ticket, network, address string) (net.Conn, string, error) {
	m.mu.Lock()
	ok := m.ready && t.epoch == m.epoch && t.country == m.cfg.Country
	active := m.current
	m.mu.Unlock()
	if !ok || active == nil {
		return nil, "", errors.New("verified tunnel is unavailable")
	}
	c, target, e := dialWorker(ctx, active.socket(), network, address)
	if e != nil {
		return nil, "", e
	}
	m.mu.Lock()
	ok = ctx.Err() == nil && m.ready && t.epoch == m.epoch && t.country == m.cfg.Country && len(m.connections) < m.cfg.MaxConnections
	if ok {
		m.connections[c] = true
	}
	m.mu.Unlock()
	if !ok {
		c.Close()
		return nil, "", errors.New("tunnel changed during dial")
	}
	return c, target, nil
}

func (m *manager) apply(c Config, swap bool) error {
	if e := c.validate(); e != nil {
		return e
	}
	m.mu.Lock()
	if c.Listen != m.cfg.Listen || c.RunDir != m.cfg.RunDir || c.DataDir != m.cfg.DataDir {
		m.mu.Unlock()
		return errors.New("restart the daemon to change its listening port or directories")
	}
	if !swap && reflect.DeepEqual(c, m.cfg) {
		m.mu.Unlock()
		return nil
	}
	if e := atomicJSON(m.configPath, c); e != nil {
		m.mu.Unlock()
		return e
	}
	if !slices.Equal(c.Sources, m.cfg.Sources) {
		m.nodesUpdated = time.Time{}
	}
	m.cfg = c
	m.swap = swap
	m.ready = false
	m.epoch++
	m.status = Status{Version: version, State: "waiting", Country: c.Country, Mode: c.Mode, Reason: "configuration changed; forwarding blocked", Updated: time.Now()}
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	m.revoke()
	select {
	case m.wake <- struct{}{}:
	default:
	}
	return nil
}

func (m *manager) run(ctx context.Context) {
	defer m.sourceClient.CloseIdleConnections()
	var done chan struct{}
	select {
	case m.wake <- struct{}{}:
	default:
	}
	for {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			if m.cancel != nil {
				m.cancel()
			}
			m.mu.Unlock()
			m.revoke()
			if done != nil {
				<-done
			}
			m.setStatus("stopped", "daemon stopped", Node{}, nil)
			return
		case <-m.wake:
			m.mu.Lock()
			if m.cancel != nil {
				m.cancel()
			}
			m.mu.Unlock()
			m.revoke()
			if done != nil {
				<-done
				done = nil
			}
			child, cancel := context.WithCancel(ctx)
			m.mu.Lock()
			c := m.cfg
			swap := m.swap
			m.swap = false
			m.cancel = cancel
			m.mu.Unlock()
			if !c.Enabled {
				cancel()
				m.setStatus("stopped", "disabled by user", Node{}, nil)
				continue
			}
			done = make(chan struct{})
			go func(finished chan struct{}) { defer close(finished); m.connectLoop(child, c, swap) }(done)
		}
	}
}

func directClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{DialContext: func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp4", address)
	}, TLSHandshakeTimeout: 8 * time.Second, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 8, MaxIdleConnsPerHost: 2, ForceAttemptHTTP2: true, DisableCompression: true}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}
func readHTTP(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return nil, errors.New("invalid HTTP request")
	}
	req.Header.Set("User-Agent", "sing-box-vpn/"+version)
	req.Header.Set("Accept-Encoding", "identity")
	res, e := client.Do(req)
	if e != nil {
		return nil, errors.New("HTTP lookup unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP response %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > limit {
		return nil, errors.New("HTTP response too large")
	}
	return b, nil
}
func (m *manager) refresh(ctx context.Context, c Config, force bool) []Node {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	started := time.Now()
	select {
	case m.refreshSlot <- struct{}{}:
		defer func() { <-m.refreshSlot }()
	case <-ctx.Done():
		return nil
	}
	m.mu.Lock()
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
		b, e := readHTTP(ctx, m.sourceClient, source, 16<<20)
		if e != nil {
			continue
		}
		nodes, e := parseNodes(b)
		if e != nil {
			continue
		}
		m.mu.Lock()
		if !slices.Equal(c.Sources, m.cfg.Sources) {
			m.mu.Unlock()
			return nil
		}
		m.nodes = nodes
		m.nodesUpdated = time.Now()
		m.mu.Unlock()
		_ = atomicJSON(filepath.Join(c.DataDir, "nodes.json"), nodes)
		return nodes
	}
	m.mu.Lock()
	nodes = m.nodes
	m.mu.Unlock()
	return nodes
}

func (m *manager) scheduleRefresh(parent context.Context, c Config, force bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refreshPending {
		return
	}
	m.refreshPending = true
	go func() {
		defer func() { m.mu.Lock(); m.refreshPending = false; m.mu.Unlock() }()
		m.refresh(parent, c, force)
	}()
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
	for _, n := range nodes {
		add(n)
		if len(pool) >= 6 {
			break
		}
	}
	if c.Mode == "fixed" && len(pool) > 1 {
		pool = pool[:1]
	}
	return pool
}

func tunnelClient(active tunnel) *http.Client {
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, _, e := dialWorker(ctx, active.socket(), "tcp", address)
		return conn, e
	}, TLSHandshakeTimeout: 8 * time.Second, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 2, MaxIdleConnsPerHost: 1, ForceAttemptHTTP2: true, DisableCompression: true}
	return &http.Client{Timeout: 12 * time.Second, Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

func (m *manager) probe(ctx context.Context, c Config, active tunnel, client *http.Client) (Geo, error) {
	if m.probeOverride != nil {
		return m.probeOverride(ctx, c, active)
	}
	b, e := readHTTP(ctx, client, c.ProbeURL, 512)
	if e != nil {
		return Geo{}, probeUnavailable("tunnel exit probe failed")
	}
	ip := strings.TrimSpace(string(b))
	if !publicIPv4(ip) {
		return Geo{}, errors.New("exit probe did not return public IPv4")
	}
	host, e := readHTTP(ctx, m.hostClient, c.ProbeURL, 512)
	if e != nil {
		return Geo{}, probeUnavailable("host exit verification unavailable")
	}
	hostIP := strings.TrimSpace(string(host))
	if !publicIPv4(hostIP) {
		return Geo{}, probeUnavailable("host exit probe did not return public IPv4")
	}
	if hostIP == ip {
		return Geo{}, errors.New("tunnel exit equals VPS exit")
	}
	cacheKey := c.GeoURL + "\x00" + ip
	m.mu.Lock()
	cached, exists := m.geoCache[cacheKey]
	m.mu.Unlock()
	if exists && time.Now().Before(cached.expires) {
		return cached.value, c.acceptGeo(cached.value)
	}
	metadata, e := readHTTP(ctx, m.hostClient, strings.Replace(c.GeoURL, "%s", ip, 1), 1<<20)
	if e != nil {
		return Geo{}, probeUnavailable("exit country verification unavailable")
	}
	g, e := parseGeo(metadata, ip)
	if e != nil {
		return g, probeUnavailable("exit country verification unavailable: " + e.Error())
	}
	m.mu.Lock()
	now := time.Now()
	oldestKey := ""
	var oldest time.Time
	for key, item := range m.geoCache {
		if !now.Before(item.expires) {
			delete(m.geoCache, key)
		} else if oldest.IsZero() || item.expires.Before(oldest) {
			oldestKey, oldest = key, item.expires
		}
	}
	if len(m.geoCache) >= 128 {
		delete(m.geoCache, oldestKey)
	}
	m.geoCache[cacheKey] = cachedGeo{value: g, expires: now.Add(15 * time.Minute)}
	m.mu.Unlock()
	return g, c.acceptGeo(g)
}

// Block forwarding on the first failure; transient lookups get two retries
// on the existing tunnel before the connection loop tears it down.
func (m *manager) recheck(ctx context.Context, c Config, active tunnel, n Node, client *http.Client) (Geo, error) {
	return m.verifyExit(ctx, c, active, n, client, true)
}

func (m *manager) verifyExit(ctx context.Context, c Config, active tunnel, n Node, client *http.Client, revokeOnFailure bool) (Geo, error) {
	for attempt := 0; ; attempt++ {
		g, err := m.probe(ctx, c, active, client)
		if ctx.Err() != nil {
			return Geo{}, context.Cause(ctx)
		}
		if err == nil {
			return g, err
		}
		if revokeOnFailure {
			m.revoke()
		}
		m.setStatus("verifying", err.Error()+"; forwarding blocked", n, nil)
		var temporary probeUnavailable
		if !errors.As(err, &temporary) || attempt == 2 {
			return g, err
		}
		if client != nil {
			client.CloseIdleConnections()
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Geo{}, context.Cause(ctx)
		case <-timer.C:
		}
	}
}

func (m *manager) reconnectCandidates(c Config, nodes []Node, swap bool) []Node {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for host, until := range m.failed {
		if !now.Before(until) {
			delete(m.failed, host)
		}
	}
	return candidates(c, nodes, m.previous, swap, m.failed)
}

func (m *manager) connectLoop(ctx context.Context, c Config, swap bool) {
	backoff := 5 * time.Second
	lastFailure := ""
	var initial []Node
	if !swap {
		m.mu.Lock()
		cached, saved := m.nodes, m.previous.Host != ""
		m.mu.Unlock()
		if saved {
			initial = m.reconnectCandidates(c, cached, false)
			if len(initial) > 0 {
				// Retry one saved candidate immediately; update its source in parallel.
				initial = []Node{initial[0]}
				m.scheduleRefresh(ctx, c, false)
			}
		}
	}
	for ctx.Err() == nil {
		pool := initial
		cachedAttempt := len(pool) > 0
		initial = nil
		if len(pool) == 0 {
			m.setStatus("fetching", "fetching same-country candidates", Node{}, nil)
			pool = m.reconnectCandidates(c, m.refresh(ctx, c, false), swap)
		}
		if len(pool) == 0 {
			reason := "no valid candidate in requested country"
			if lastFailure != "" {
				reason += "; last tunnel failure: " + lastFailure
			}
			m.setStatus("waiting", reason, Node{}, nil)
		}
		for _, n := range pool {
			if ctx.Err() != nil {
				return
			}
			m.mu.Lock()
			sessionEpoch := m.epoch
			m.mu.Unlock()
			m.setStatus("connecting", "OpenVPN handshake", n, nil)
			active := m.factory(c)
			session, stop := context.WithCancelCause(ctx)
			var client *http.Client
			e := active.start(session, n)
			if e == nil {
				// Observe death during startup verification as well as between health
				// checks. A failed process must not retain a 12-second probe and retries.
				go watchTunnel(session, active, stop)
				client = tunnelClient(active)
				m.setStatus("verifying", "verifying exit country and provider", n, nil)
				var g Geo
				// Startup already blocks forwarding; retries must retain its epoch.
				g, e = m.verifyExit(session, c, active, n, client, false)
				if e == nil && session.Err() == nil {
					m.mu.Lock()
					if session.Err() != nil || sessionEpoch != m.epoch || m.cfg.Country != c.Country {
						m.mu.Unlock()
						stop(nil)
						client.CloseIdleConnections()
						active.stop()
						return
					}
					m.current = active
					m.previous = n
					m.ready = true
					m.mu.Unlock()
					// An implicit fixed node is already persisted in last-node.json;
					// rewriting gateway.json here could overwrite a concurrent menu edit.
					_ = atomicJSON(filepath.Join(c.DataDir, "last-node.json"), n)
					m.setStatus("ready", "volunteer / residential candidate", n, &g)
					backoff = 5 * time.Second
					swap = false
					e = m.monitorSession(session, c, active, n, client)
				}
			}
			m.revoke()
			stop(nil)
			if client != nil {
				client.CloseIdleConnections()
			}
			active.stop()
			m.mu.Lock()
			m.current = nil
			m.mu.Unlock()
			if ctx.Err() != nil {
				return
			}
			reason := "connection failed"
			if e != nil {
				reason = e.Error()
			}
			lastFailure = reason
			m.setStatus("waiting", reason, n, nil)
			m.mu.Lock()
			m.failed[n.Host] = time.Now().Add(2 * time.Minute)
			m.mu.Unlock()
		}
		if cachedAttempt {
			continue
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 60*time.Second {
			backoff *= 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
		}
	}
}

func watchTunnel(ctx context.Context, active tunnel, cancel context.CancelCauseFunc) {
	select {
	case <-active.died():
		cancel(errors.New("tunnel process stopped; forwarding blocked"))
	case <-ctx.Done():
	}
}

func (m *manager) monitor(ctx context.Context, c Config, active tunnel, n Node, client *http.Client) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go watchTunnel(ctx, active, cancel)
	return m.monitorSession(ctx, c, active, n, client)
}

func (m *manager) monitorSession(ctx context.Context, c Config, active tunnel, n Node, client *http.Client) error {
	tick := time.NewTicker(time.Duration(c.HealthInterval) * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-tick.C:
			g, e := m.recheck(ctx, c, active, n, client)
			if ctx.Err() != nil {
				return context.Cause(ctx)
			}
			if e != nil {
				return e
			}
			m.mu.Lock()
			if ctx.Err() != nil || m.current != active || m.cfg.Country != c.Country {
				m.mu.Unlock()
				return errors.New("tunnel changed during health verification")
			}
			m.ready = true
			m.mu.Unlock()
			m.setStatus("ready", "volunteer / residential candidate", n, &g)
		}
	}
}
