// Package gatewayconfig owns the VPNGate contract without panel or host dependencies.
package gatewayconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const MaxConfigBytes = 1 << 20

type Range struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

var Limits = struct {
	ConnectTimeout Range `json:"connect_timeout"`
	HealthInterval Range `json:"health_interval"`
	MaxConnections Range `json:"max_connections"`
	Sources        Range `json:"sources"`
}{Range{5, 180}, Range{1, 300}, Range{1, 1024}, Range{1, 16}}

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

func Default() Config {
	return Config{Country: "JP", Mode: "sticky", Listen: "127.0.0.1:7928", DataDir: "/var/lib/yg-vpngate", RunDir: "/run/yg-vpngate", Sources: []string{"https://www.vpngate.net/api/iphone/"}, GeoURL: "https://api.ipapi.is/?q=%s", ProbeURL: "https://api.ipify.org", DNS: "1.1.1.1:53", ResidentialOnly: true, ExcludeHosting: true, ConnectTimeout: 60, HealthInterval: 10, MaxConnections: 128}
}

func Decode(data []byte) (Config, error) {
	c := Default()
	if len(data) > MaxConfigBytes {
		return c, errors.New("gateway configuration exceeds 1 MiB")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return c, errors.New("gateway configuration must be an object")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return c, errors.New("gateway configuration must contain exactly one JSON object")
	}
	return c, nil
}

var country = regexp.MustCompile(`^[A-Z]{2}$`)

func (c *Config) ValidatePolicy() error {
	c.Country = strings.ToUpper(strings.TrimSpace(c.Country))
	if !country.MatchString(c.Country) {
		return errors.New("country must be a two-letter ISO code")
	}
	if c.Mode != "sticky" && c.Mode != "fixed" {
		return errors.New("mode must be sticky or fixed")
	}
	for _, asn := range c.AllowASNs {
		if asn <= 0 || uint64(asn) > 4294967295 {
			return errors.New("ASN allowlist entries must be positive 32-bit integers")
		}
	}
	for _, value := range []struct {
		name  string
		value int
		limit Range
	}{
		{"connect_timeout", c.ConnectTimeout, Limits.ConnectTimeout},
		{"health_interval", c.HealthInterval, Limits.HealthInterval},
		{"max_connections", c.MaxConnections, Limits.MaxConnections},
		{"sources", len(c.Sources), Limits.Sources},
	} {
		if value.value < value.limit.Min || value.value > value.limit.Max {
			return fmt.Errorf("%s must be within %d–%d", value.name, value.limit.Min, value.limit.Max)
		}
	}
	for _, source := range c.Sources {
		if !HTTPURL(source, false) {
			return errors.New("node source must use HTTP(S)")
		}
	}
	if strings.Count(c.GeoURL, "%s") != 1 || !HTTPURL(strings.Replace(c.GeoURL, "%s", "1.1.1.1", 1), false) {
		return errors.New("geo_url must be an HTTP(S) template with one %s")
	}
	if !HTTPURL(c.ProbeURL, true) {
		return errors.New("exit probe must use HTTPS")
	}
	host, port, err := net.SplitHostPort(c.DNS)
	ip, ipErr := netip.ParseAddr(host)
	if err != nil || ipErr != nil || !PublicIPv4(ip.Unmap()) || !validPort(port) {
		return errors.New("DNS must be a public IPv4 resolver with port")
	}
	return nil
}

func (c *Config) Validate() error {
	if err := c.ValidatePolicy(); err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil || host != "127.0.0.1" || !validPort(port) {
		return errors.New("proxy must listen on an IPv4 loopback TCP port")
	}
	if len(c.ProxyUser) < 1 || len(c.ProxyUser) > 255 || len(c.ProxyPass) < 32 || len(c.ProxyPass) > 255 {
		return errors.New("proxy credentials missing or too short")
	}
	for _, directory := range []string{c.DataDir, c.RunDir} {
		clean := filepath.ToSlash(filepath.Clean(directory))
		// Allow Linux deployment paths during Windows development as well.
		if (!filepath.IsAbs(directory) && !strings.HasPrefix(clean, "/")) || clean == "/" || filepath.Dir(filepath.Clean(directory)) == filepath.Clean(directory) {
			return errors.New("data and runtime directories must be dedicated absolute paths")
		}
	}
	c.DataDir, c.RunDir = filepath.Clean(c.DataDir), filepath.Clean(c.RunDir)
	return nil
}

func validPort(value string) bool {
	port, err := strconv.Atoi(value)
	return err == nil && port > 0 && port <= 65535
}
func HTTPURL(value string, httpsOnly bool) bool {
	u, err := url.Parse(value)
	return err == nil && u.Hostname() != "" && (u.Scheme == "https" || !httpsOnly && u.Scheme == "http") && u.User == nil && u.Fragment == "" && (u.Port() == "" || validPort(u.Port()))
}
func PublicIPv4(ip netip.Addr) bool {
	if !ip.Is4() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	v := ip.As4()
	return !(v[0] == 100 && v[1] >= 64 && v[1] <= 127)
}
