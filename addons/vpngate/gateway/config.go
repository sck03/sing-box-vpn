package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
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
	input := &io.LimitedReader{R: f, N: (1 << 20) + 1}
	d := json.NewDecoder(input)
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	err = d.Decode(&struct{}{})
	if input.N == 0 {
		return c, errors.New("gateway configuration exceeds 1 MiB")
	}
	if err != io.EOF {
		return c, errors.New("gateway configuration must contain exactly one JSON object")
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
	for _, asn := range c.AllowASNs {
		if asn <= 0 || uint64(asn) > maxASN {
			return errors.New("ASN allowlist entries must be positive 32-bit integers")
		}
	}
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil || host != "127.0.0.1" || !validPort(port) {
		return errors.New("proxy must listen on an IPv4 loopback TCP port")
	}
	if len(c.ProxyUser) < 1 || len(c.ProxyUser) > 255 || len(c.ProxyPass) < 32 || len(c.ProxyPass) > 255 {
		return errors.New("proxy credentials missing or too short")
	}
	c.DataDir, c.RunDir = filepath.Clean(c.DataDir), filepath.Clean(c.RunDir)
	if !filepath.IsAbs(c.DataDir) || !filepath.IsAbs(c.RunDir) || filepath.Dir(c.DataDir) == c.DataDir || filepath.Dir(c.RunDir) == c.RunDir {
		return errors.New("data and runtime directories must be dedicated absolute paths")
	}
	if c.ConnectTimeout < 5 || c.ConnectTimeout > 180 || c.HealthInterval < 1 || c.HealthInterval > 300 || c.MaxConnections < 1 || c.MaxConnections > 1024 {
		return errors.New("invalid timeout or connection limits")
	}
	if len(c.Sources) == 0 || len(c.Sources) > 16 {
		return errors.New("configure 1-16 VPNGate sources")
	}
	for _, s := range c.Sources {
		if !validHTTPURL(s, false) {
			return errors.New("node source must use HTTP(S)")
		}
	}
	if strings.Count(c.GeoURL, "%s") != 1 || !validHTTPURL(strings.Replace(c.GeoURL, "%s", "1.1.1.1", 1), false) {
		return errors.New("geo_url must be an HTTP(S) template with one %s")
	}
	if !validHTTPURL(c.ProbeURL, true) {
		return errors.New("exit probe must use HTTPS")
	}
	dh, dp, e := net.SplitHostPort(c.DNS)
	if e != nil || !publicIPv4(dh) || !validPort(dp) {
		return errors.New("DNS must be a public IPv4 resolver with port")
	}
	return nil
}

func validHTTPURL(value string, httpsOnly bool) bool {
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	return u.Hostname() != "" && (u.Scheme == "https" || !httpsOnly && u.Scheme == "http") && (u.Port() == "" || validPort(u.Port()))
}

func atomicJSON(path string, value any) error {
	return manage.AtomicJSON(path, value)
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
