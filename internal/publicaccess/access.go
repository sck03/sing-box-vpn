// Package publicaccess defines the transport policy shared by public listeners.
package publicaccess

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/tlsconfig"
)

type Options struct {
	Listen       string   `json:"listen"`
	PublicURL    string   `json:"public_url"`
	Certificate  string   `json:"certificate"`
	Key          string   `json:"key"`
	TrustedProxy []string `json:"trusted_proxy"`
}

type Rules struct{ Development, AllowProxyPath, RequireHTTPS bool }

func (o Options) Port() int {
	_, port, _ := net.SplitHostPort(o.Listen)
	n, _ := strconv.Atoi(port)
	return n
}
func (o Options) Origin() string {
	if o.PublicURL != "" {
		return o.PublicURL
	}
	return "http://" + o.Listen
}
func LoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}
func ShareableURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || LoopbackHost(u.Hostname()) {
		return errors.New("请先在公网访问配置可访问的 HTTPS 域名或 IP；本机地址不能分享给其他人")
	}
	return nil
}

func (o Options) Validate(rules Rules) error {
	host, port, err := net.SplitHostPort(o.Listen)
	ip := net.ParseIP(host)
	n, parseErr := strconv.Atoi(port)
	if err != nil || ip == nil || parseErr != nil || n < 0 || n > 65535 || (n == 0 && (!rules.Development || !ip.IsLoopback())) {
		return errors.New("监听地址需要有效 IP:端口")
	}
	if (o.Certificate == "") != (o.Key == "") {
		return errors.New("请同时填写 TLS 证书与私钥")
	}
	if o.Certificate != "" && len(o.TrustedProxy) > 0 {
		return errors.New("请选择直接 HTTPS 或可信 HTTPS 反向代理")
	}
	for _, value := range o.TrustedProxy {
		if _, err := netip.ParsePrefix(value); err != nil {
			return errors.New("可信代理需要 CIDR，例如 127.0.0.1/32")
		}
	}
	tlsTransport := o.Certificate != "" || len(o.TrustedProxy) > 0
	if !tlsTransport && (!ip.IsLoopback() || rules.RequireHTTPS) {
		return errors.New("公网服务需要内置 TLS 或明确的可信 HTTPS 反向代理")
	}
	if o.PublicURL == "" && (tlsTransport || !ip.IsLoopback() || rules.RequireHTTPS) {
		return errors.New("请填写 HTTPS 公网 URL")
	}
	if o.PublicURL != "" {
		u, err := url.Parse(o.PublicURL)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || !slices.Contains([]string{"http", "https"}, u.Scheme) {
			return errors.New("公网 URL 格式无效")
		}
		if u.Path != "" && !(rules.AllowProxyPath && len(o.TrustedProxy) > 0) {
			return errors.New("只有反向代理订阅地址可以包含路径前缀")
		}
		if (u.Scheme == "https") != tlsTransport {
			return errors.New("HTTPS 地址必须对应 TLS 或可信 HTTPS 代理")
		}
		if tlsTransport || rules.RequireHTTPS {
			if err := ShareableURL(o.PublicURL); err != nil {
				return err
			}
		} else if !LoopbackHost(u.Hostname()) {
			return errors.New("公网地址需要 HTTPS")
		}
		if o.Certificate != "" {
			pair, err := tlsconfig.Read(o.Certificate, o.Key, u.Hostname())
			if err != nil {
				return err
			}
			return tlsconfig.ValidAt(pair, time.Now())
		}
	}
	return nil
}

func (o Options) TLS() *tls.Config {
	u, _ := url.Parse(o.Origin())
	cache := &tlsconfig.Cache{Certificate: o.Certificate, Key: o.Key, Domain: u.Hostname()}
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: cache.GetCertificate}
}

// ProxyPeer verifies the immediate hop before trusting forwarded headers.
type ProxyPolicy []netip.Prefix

func NewProxyPolicy(values []string) (ProxyPolicy, error) {
	policy := make(ProxyPolicy, 0, len(values))
	for _, value := range values {
		p, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, err
		}
		policy = append(policy, p)
	}
	return policy, nil
}
func (p ProxyPolicy) Peer(r *http.Request) (string, error) {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "", errors.New("invalid remote address")
	}
	if len(p) == 0 {
		return peer, nil
	}
	ip, err := netip.ParseAddr(peer)
	if err != nil || !slices.ContainsFunc(p, func(prefix netip.Prefix) bool { return prefix.Contains(ip.Unmap()) }) || r.Header.Get("X-Forwarded-Proto") != "https" {
		return "", errors.New("untrusted HTTPS proxy")
	}
	return ForwardedPeer(r.Header.Get("X-Forwarded-For"), peer, p), nil
}
func (o Options) ProxyPeer(r *http.Request) (string, error) {
	p, err := NewProxyPolicy(o.TrustedProxy)
	if err != nil {
		return "", err
	}
	return p.Peer(r)
}

func ForwardedPeer(header, fallback string, trusted []netip.Prefix) string {
	parts := strings.Split(header, ",")
	if len(parts) > 32 {
		return fallback
	}
	for i := len(parts) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			return fallback
		}
		ip = ip.Unmap()
		if !slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(ip) }) {
			return ip.String()
		}
	}
	return fallback
}

func (o Options) Protect(next http.Handler) http.Handler {
	u, _ := url.Parse(o.Origin())
	policy, policyErr := NewProxyPolicy(o.TrustedProxy)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u == nil || r.Host != u.Host {
			http.Error(w, "unexpected Host", 400)
			return
		}
		if policyErr != nil {
			http.Error(w, "invalid proxy policy", 500)
			return
		}
		if _, err := policy.Peer(r); err != nil {
			http.Error(w, err.Error(), 403)
			return
		}
		if u.Scheme == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}
