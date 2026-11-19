package subscription

import (
	"errors"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
)

type DNSPolicy struct {
	Local        []string `json:"local,omitempty"`
	Remote       []string `json:"remote,omitempty"`
	Bootstrap    []string `json:"bootstrap,omitempty"`
	FakeIP       bool     `json:"fake_ip"`
	FakeIPFilter []string `json:"fake_ip_filter,omitempty"`
	IPv6         bool     `json:"ipv6"`
}

func validateResolver(value string, bootstrap bool) error {
	if _, err := netip.ParseAddr(value); err == nil {
		return nil
	}
	if bootstrap {
		return errors.New("bootstrap DNS requires IP addresses")
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || !slices.Contains([]string{"udp", "tcp", "tls", "https"}, u.Scheme) {
		return errors.New("DNS resolver requires an IP or udp/tcp/tls/https URL")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return errors.New("invalid DNS port")
		}
	}
	if u.Scheme != "https" && u.Path != "" && u.Path != "/" {
		return errors.New("DNS transport does not support a path")
	}
	return nil
}

func (p DNSPolicy) Validate() error {
	if len(p.Local)+len(p.Remote)+len(p.Bootstrap) > 16 || len(p.FakeIPFilter) > 256 {
		return errors.New("DNS policy size limit exceeded")
	}
	for _, list := range [][]string{p.Local, p.Remote} {
		for _, value := range list {
			if err := validateResolver(value, false); err != nil {
				return err
			}
		}
	}
	for _, value := range p.Bootstrap {
		if err := validateResolver(value, true); err != nil {
			return err
		}
	}
	if p.FakeIP {
		return nil
	}
	if len(p.FakeIPFilter) > 0 {
		return errors.New("fake-IP exclusions require fake-IP mode")
	}
	return nil
}

func dnsPolicy(t Template) DNSPolicy {
	p := DNSPolicy{Local: []string{"223.5.5.5"}, Remote: templateDNS(t), Bootstrap: []string{"223.5.5.5"}, FakeIP: true, FakeIPFilter: []string{"*.lan", "localhost", "+.local"}}
	if t.DNSPolicy != nil {
		if len(t.DNSPolicy.Local) > 0 {
			p.Local = t.DNSPolicy.Local
		}
		if len(t.DNSPolicy.Remote) > 0 {
			p.Remote = t.DNSPolicy.Remote
		}
		if len(t.DNSPolicy.Bootstrap) > 0 {
			p.Bootstrap = t.DNSPolicy.Bootstrap
		}
		p.FakeIP, p.IPv6, p.FakeIPFilter = t.DNSPolicy.FakeIP, t.DNSPolicy.IPv6, t.DNSPolicy.FakeIPFilter
	}
	return p
}

func singDNS(value, tag, detour string) Object {
	result := Object{"type": "udp", "tag": tag, "server": value}
	if u, err := url.Parse(value); err == nil && u.Hostname() != "" {
		result["type"], result["server"] = u.Scheme, u.Hostname()
		if _, err := netip.ParseAddr(u.Hostname()); err != nil {
			result["domain_resolver"] = "bootstrap"
		}
		if u.Port() != "" {
			result["server_port"] = integer(u.Port())
		}
		if u.Scheme == "https" {
			result["path"] = fallback(u.Path, "/dns-query")
		}
	}
	if detour != "" {
		result["detour"] = detour
	}
	return result
}
