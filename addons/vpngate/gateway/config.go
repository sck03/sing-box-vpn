package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/manage"
)

type Config struct {
	Country         string   `json:"country"`
	Enabled         bool     `json:"enabled"`
	Mode            string   `json:"mode"`
	FixedHost       string   `json:"fixed_host,omitempty"`
	Listen          string   `json:"listen"`
	ProxyUser       string   `json:"proxy_user"`
	ProxyPass       string   `json:"proxy_pass"`
	DataDir         string   `json:"data_dir"`
	RunDir          string   `json:"run_dir"`
	Sources         []string `json:"sources"`
	GeoURL          string   `json:"geo_url"`
	ProbeURL        string   `json:"probe_url"`
	DNS             string   `json:"dns"`
	ResidentialOnly bool     `json:"residential_only"`
	ExcludeHosting  bool     `json:"exclude_hosting"`
	RequireISP      bool     `json:"require_isp"`
	AllowASNs       []int    `json:"allow_asns,omitempty"`
	ConnectTimeout  int      `json:"connect_timeout"`
	HealthInterval  int      `json:"health_interval"`
	MaxConnections  int      `json:"max_connections"`
}

func defaults() Config {
	return Config{Country: "JP", Mode: "sticky", Listen: "127.0.0.1:7928", DataDir: "/var/lib/yg-vpngate", RunDir: "/run/yg-vpngate", Sources: []string{"https://www.vpngate.net/api/iphone/"}, GeoURL: "https://api.ipapi.is/?q=%s", ProbeURL: "https://api.ipify.org", DNS: "1.1.1.1:53", ResidentialOnly: true, ExcludeHosting: true, ConnectTimeout: 60, HealthInterval: 10, MaxConnections: 128}
}

func readConfig(path string) (Config, error) {
	c := defaults()
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	return c, c.validate()
}
func (c *Config) validate() error {
	c.Country = strings.ToUpper(strings.TrimSpace(c.Country))
	if !countryPattern.MatchString(c.Country) {
		return errors.New("country must be a two-letter ISO code")
	}
	if c.Mode != "sticky" && c.Mode != "fixed" {
		return errors.New("mode must be sticky or fixed")
	}
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil || host != "127.0.0.1" || !positiveInt(port) {
		return errors.New("proxy must listen on an IPv4 loopback TCP port")
	}
	if len(c.ProxyUser) < 1 || len(c.ProxyUser) > 255 || len(c.ProxyPass) < 32 || len(c.ProxyPass) > 255 {
		return errors.New("proxy credentials missing or too short")
	}
	if !filepath.IsAbs(c.DataDir) || !filepath.IsAbs(c.RunDir) || c.DataDir == "/" || c.RunDir == "/" {
		return errors.New("data and runtime directories must be dedicated absolute paths")
	}
	if c.ConnectTimeout < 5 || c.ConnectTimeout > 180 || c.HealthInterval < 1 || c.HealthInterval > 300 || c.MaxConnections < 1 || c.MaxConnections > 1024 {
		return errors.New("invalid timeout or connection limits")
	}
	if len(c.Sources) == 0 || len(c.Sources) > 16 {
		return errors.New("configure 1-16 VPNGate sources")
	}
	for _, s := range c.Sources {
		if !strings.HasPrefix(s, "https://") && !strings.HasPrefix(s, "http://") {
			return errors.New("node source must use HTTP(S)")
		}
	}
	if strings.Count(c.GeoURL, "%s") != 1 || (!strings.HasPrefix(c.GeoURL, "https://") && !strings.HasPrefix(c.GeoURL, "http://")) {
		return errors.New("geo_url must be an HTTP(S) template with one %s")
	}
	if !strings.HasPrefix(c.ProbeURL, "https://") {
		return errors.New("exit probe must use HTTPS")
	}
	dh, dp, e := net.SplitHostPort(c.DNS)
	if e != nil || !publicIPv4(dh) || !positiveInt(dp) {
		return errors.New("DNS must be a public IPv4 resolver with port")
	}
	return nil
}

func atomicJSON(path string, value any) error {
	return manage.AtomicJSON(path, value)
}

type Geo struct {
	IP           string `json:"ip"`
	Country      string `json:"country"`
	ASN          int    `json:"asn"`
	ISP          string `json:"isp"`
	Hosting      bool   `json:"hosting"`
	HostingKnown bool   `json:"hosting_known"`
	ASNType      string `json:"asn_type"`
}
type Status struct {
	Version string    `json:"version"`
	State   string    `json:"state"`
	Country string    `json:"country"`
	Mode    string    `json:"mode"`
	Node    string    `json:"node,omitempty"`
	Exit    *Geo      `json:"exit,omitempty"`
	Reason  string    `json:"reason,omitempty"`
	Updated time.Time `json:"updated"`
}

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
	if n, ok := asn["asn"].(float64); ok {
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
