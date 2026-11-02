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
func (b *backend) died() <-chan error { return b.ovpnDone }

type manager struct {
	mu            sync.Mutex
	cfg           Config
	configPath    string
	status        Status
	ready         bool
	epoch         uint64
	current       tunnel
	previous      Node
	nodes         []Node
	connections   map[net.Conn]bool
	wake          chan struct{}
	cancel        context.CancelFunc
	swap          bool
	factory       func(Config) tunnel
	probeOverride func(context.Context, Config, tunnel) (Geo, error)
	geoCache      map[string]cachedGeo
	failed        map[string]time.Time
}
type cachedGeo struct {
	value   Geo
	expires time.Time
}

func newManager(c Config, path string) *manager {
	m := &manager{cfg: c, configPath: path, status: Status{Version: version, State: "stopped", Country: c.Country, Mode: c.Mode, Updated: time.Now()}, connections: map[net.Conn]bool{}, wake: make(chan struct{}, 1), factory: func(c Config) tunnel { return &backend{cfg: c} }}
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
	if s.Exit != nil {
		g := *s.Exit
		s.Exit = &g
	}
	return s
}
func (m *manager) setStatus(state, reason string, node Node, geo *Geo) {
	m.mu.Lock()
	m.status = Status{Version: version, State: state, Reason: reason, Country: m.cfg.Country, Mode: m.cfg.Mode, Node: node.Host, Exit: geo, Updated: time.Now()}
	s := m.status
	dir := m.cfg.DataDir
	m.mu.Unlock()
	_ = atomicJSON(filepath.Join(dir, "state.json"), s)
}
func (m *manager) revoke() {
	m.mu.Lock()
	m.ready = false
	m.epoch++
	all := make([]net.Conn, 0, len(m.connections))
	for c := range m.connections {
		all = append(all, c)
	}
	m.mu.Unlock()
	for _, c := range all {
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
	if !m.allowed(t) || !m.track(c) {
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
	if e := atomicJSON(m.configPath, c); e != nil {
		m.mu.Unlock()
		return e
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
	var done chan struct{}
	m.wake <- struct{}{}
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
			m.mu.Lock()
			c := m.cfg
			swap := m.swap
			m.swap = false
			m.mu.Unlock()
			if !c.Enabled {
				m.setStatus("stopped", "disabled by user", Node{}, nil)
				continue
			}
			child, cancel := context.WithCancel(ctx)
			m.mu.Lock()
			m.cancel = cancel
			m.mu.Unlock()
			done = make(chan struct{})
			go func() { defer close(done); m.connectLoop(child, c, swap) }()
		}
	}
}

func directClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{DialContext: func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp4", address)
	}}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
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
func (m *manager) refresh(ctx context.Context, c Config) []Node {
	client := directClient(25 * time.Second)
	defer client.CloseIdleConnections()
	for _, source := range c.Sources {
		b, e := readHTTP(ctx, client, source, 16<<20)
		if e != nil {
			continue
		}
		nodes, e := parseNodes(b)
		if e != nil {
			continue
		}
		m.mu.Lock()
		m.nodes = nodes
		m.mu.Unlock()
		_ = atomicJSON(filepath.Join(c.DataDir, "nodes.json"), nodes)
		return nodes
	}
	m.mu.Lock()
	nodes := append([]Node(nil), m.nodes...)
	m.mu.Unlock()
	return nodes
}

func candidates(c Config, nodes []Node, previous Node, swap bool) []Node {
	var pool []Node
	seen := map[string]bool{}
	accept := func(n Node) bool {
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
		for _, n := range append([]Node{previous}, nodes...) {
			if (c.FixedHost != "" && n.Host == c.FixedHost) || (c.FixedHost == "" && n.Host == previous.Host && n.Host != "") {
				add(n)
				if len(pool) > 0 || c.FixedHost != "" {
					return pool
				}
			}
		}
		if c.FixedHost != "" {
			return nil
		}
	}
	if !swap {
		add(previous)
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

func (m *manager) probe(ctx context.Context, c Config, active tunnel) (Geo, error) {
	if m.probeOverride != nil {
		return m.probeOverride(ctx, c, active)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, _, e := dialWorker(ctx, active.socket(), "tcp", address)
		return conn, e
	}}
	client := &http.Client{Timeout: 12 * time.Second, Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	defer transport.CloseIdleConnections()
	b, e := readHTTP(ctx, client, c.ProbeURL, 512)
	if e != nil {
		return Geo{}, errors.New("tunnel exit probe failed")
	}
	ip := strings.TrimSpace(string(b))
	if !publicIPv4(ip) {
		return Geo{}, errors.New("exit probe did not return public IPv4")
	}
	hostClient := directClient(12 * time.Second)
	defer hostClient.CloseIdleConnections()
	host, e := readHTTP(ctx, hostClient, c.ProbeURL, 512)
	if e != nil {
		return Geo{}, errors.New("host exit verification unavailable")
	}
	if strings.TrimSpace(string(host)) == ip {
		return Geo{}, errors.New("tunnel exit equals VPS exit")
	}
	m.mu.Lock()
	cached, exists := m.geoCache[ip]
	m.mu.Unlock()
	if exists && time.Now().Before(cached.expires) {
		return cached.value, c.acceptGeo(cached.value)
	}
	metadata, e := readHTTP(ctx, hostClient, fmt.Sprintf(c.GeoURL, ip), 1<<20)
	if e != nil {
		return Geo{}, errors.New("exit country verification unavailable")
	}
	g, e := parseGeo(metadata, ip)
	if e != nil {
		return g, e
	}
	m.mu.Lock()
	m.geoCache[ip] = cachedGeo{value: g, expires: time.Now().Add(15 * time.Minute)}
	if len(m.geoCache) > 128 {
		for key, item := range m.geoCache {
			if time.Now().After(item.expires) {
				delete(m.geoCache, key)
			}
		}
	}
	m.mu.Unlock()
	return g, c.acceptGeo(g)
}

func (m *manager) connectLoop(ctx context.Context, c Config, swap bool) {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		m.setStatus("fetching", "fetching same-country candidates", Node{}, nil)
		nodes := m.refresh(ctx, c)
		m.mu.Lock()
		previous := m.previous
		if c.Mode != "fixed" {
			filtered := make([]Node, 0, len(nodes))
			for _, n := range nodes {
				if until, failed := m.failed[n.Host]; !failed || time.Now().After(until) {
					filtered = append(filtered, n)
				}
			}
			nodes = filtered
			if until, failed := m.failed[previous.Host]; failed && time.Now().Before(until) {
				previous = Node{}
			}
		}
		m.mu.Unlock()
		pool := candidates(c, nodes, previous, swap)
		if len(pool) == 0 {
			m.setStatus("waiting", "no valid candidate in requested country", Node{}, nil)
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
			session, stop := context.WithCancel(ctx)
			e := active.start(session, n)
			if e == nil {
				m.setStatus("verifying", "verifying exit country and provider", n, nil)
				var g Geo
				g, e = m.probe(session, c, active)
				if e == nil && session.Err() == nil {
					m.mu.Lock()
					if session.Err() != nil || sessionEpoch != m.epoch || m.cfg.Country != c.Country {
						m.mu.Unlock()
						stop()
						active.stop()
						return
					}
					m.current = active
					m.previous = n
					m.ready = true
					m.mu.Unlock()
					_ = atomicJSON(filepath.Join(c.DataDir, "last-node.json"), n)
					if c.Mode == "fixed" && c.FixedHost == "" {
						c.FixedHost = n.Host
						m.mu.Lock()
						m.cfg.FixedHost = n.Host
						_ = atomicJSON(m.configPath, m.cfg)
						m.mu.Unlock()
					}
					m.setStatus("ready", "volunteer / residential candidate", n, &g)
					backoff = 5 * time.Second
					swap = false
					e = m.monitor(session, c, active, n)
				}
			}
			m.revoke()
			stop()
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
			m.setStatus("waiting", reason, n, nil)
			m.mu.Lock()
			m.failed[n.Host] = time.Now().Add(2 * time.Minute)
			m.mu.Unlock()
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

func (m *manager) monitor(ctx context.Context, c Config, active tunnel, n Node) error {
	tick := time.NewTicker(time.Duration(c.HealthInterval) * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-active.died():
			return errors.New("OpenVPN stopped; forwarding blocked")
		case <-tick.C:
			g, e := m.probe(ctx, c, active)
			if e != nil {
				return e
			}
			m.setStatus("ready", "volunteer / residential candidate", n, &g)
		}
	}
}
