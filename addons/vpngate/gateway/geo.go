package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

type Geo struct {
	IP           string `json:"ip"`
	Country      string `json:"country"`
	ASN          int    `json:"asn"`
	ISP          string `json:"isp"`
	Hosting      bool   `json:"hosting"`
	HostingKnown bool   `json:"hosting_known"`
	ASNType      string `json:"asn_type"`
}
type cachedGeo struct {
	value   Geo
	expires time.Time
}

type probeUnavailable string

func (e probeUnavailable) Error() string { return string(e) }

func (c Config) acceptGeo(g Geo) error {
	if g.Country != c.Country {
		return fmt.Errorf("exit country %s does not match %s", g.Country, c.Country)
	}
	if c.ExcludeHosting && g.Hosting {
		return errors.New("exit is classified as hosting")
	}
	if c.RequireISP && (g.ASNType != "isp" || !g.HostingKnown) {
		return errors.New("ISP verification unavailable")
	}
	if len(c.AllowASNs) > 0 {
		for _, a := range c.AllowASNs {
			if a == g.ASN {
				return nil
			}
		}
		return errors.New("exit ASN is outside the allowlist")
	}
	return nil
}

func (m *manager) probe(ctx context.Context, c Config, active tunnel, client *http.Client) (Geo, error) {
	if m.probeOverride != nil {
		return m.probeOverride(ctx, c, active)
	}
	b, e := readHTTP(ctx, client, c.ProbeURL, 512)
	if e != nil {
		return Geo{}, probeUnavailable("tunnel exit probe failed")
	}
	address, err := netip.ParseAddr(strings.TrimSpace(string(b)))
	address = address.Unmap()
	if err != nil || !publicIPv4Addr(address) {
		return Geo{}, errors.New("exit probe did not return public IPv4")
	}
	host, e := readHTTP(ctx, m.hostClient, c.ProbeURL, 512)
	if e != nil {
		return Geo{}, probeUnavailable("host exit verification unavailable")
	}
	hostAddress, err := netip.ParseAddr(strings.TrimSpace(string(host)))
	hostAddress = hostAddress.Unmap()
	if err != nil || !publicIPv4Addr(hostAddress) {
		return Geo{}, probeUnavailable("host exit probe did not return public IPv4")
	}
	if hostAddress == address {
		return Geo{}, errors.New("tunnel exit equals VPS exit")
	}
	ip := address.String()
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
