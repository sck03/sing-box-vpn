package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

const maxASN = 1<<32 - 1

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

func parseGeo(b []byte, ip string) (Geo, error) {
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		return Geo{}, err
	}
	if ok, exists := v["success"]; exists && ok == false {
		return Geo{}, errors.New("geolocation lookup failed")
	}
	if v["error"] != nil && v["error"] != false && v["error"] != "" {
		return Geo{}, errors.New("geolocation provider reported an error")
	}
	reported, exists := v["ip"]
	if !exists {
		reported, exists = v["query"]
	}
	if exists {
		address, _ := reported.(string)
		if parsed := net.ParseIP(address); parsed == nil || !parsed.Equal(net.ParseIP(ip)) {
			return Geo{}, errors.New("geolocation address does not match the tunnel exit")
		}
	}
	g := Geo{IP: ip}
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }
	obj := func(m map[string]any, k string) map[string]any { a, _ := m[k].(map[string]any); return a }
	g.Country = str(v, "country_code")
	if g.Country == "" {
		g.Country = str(v, "countryCode")
	}
	if g.Country == "" {
		g.Country = str(obj(v, "location"), "country_code")
	}
	if g.Country == "" {
		g.Country = str(v, "country")
	}
	g.Country = strings.ToUpper(g.Country)
	if !countryPattern.MatchString(g.Country) {
		return g, errors.New("geolocation country missing")
	}
	for _, key := range []string{"is_datacenter", "hosting"} {
		if x, ok := v[key].(bool); ok {
			g.HostingKnown = true
			g.Hosting = g.Hosting || x
		}
	}
	asn := obj(v, "asn")
	connection := obj(v, "connection")
	if asn == nil {
		asn = connection
	}
	if value, exists := asn["asn"]; exists {
		n, ok := value.(float64)
		if !ok || n < 1 || n > maxASN || n != math.Trunc(n) {
			return g, errors.New("geolocation ASN must be a positive 32-bit integer")
		}
		g.ASN = int(n)
	}
	g.ASNType = strings.ToLower(str(asn, "type"))
	g.ISP = str(asn, "org")
	if g.ISP == "" {
		g.ISP = str(asn, "isp")
	}
	if g.ISP == "" {
		g.ISP = str(v, "isp")
	}
	if g.ASNType == "hosting" || g.ASNType == "datacenter" {
		g.Hosting = true
		g.HostingKnown = true
	}
	if g.ASNType == "isp" {
		g.HostingKnown = true
	}
	if g.ISP == "" {
		g.ISP = fmt.Sprintf("AS%d", g.ASN)
	}
	return g, nil
}

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
