package manage

import (
	"context"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"slices"
)

func (s *PlatformService) Preset(ctx context.Context, protocol, id string) (ManagedNode, error) {
	if !domain.ResourceID.MatchString(id) || !slices.Contains(Protocols, protocol) {
		return ManagedNode{}, errors.New("invalid preset")
	}
	state, err := s.Store.State(ctx)
	if err != nil {
		return ManagedNode{}, err
	}
	c, err := decodePlatform(state.Desired)
	if err != nil {
		return ManagedNode{}, err
	}
	used, ranges, err := allocationPorts(s.Root, c.App, c.VPN)
	if err != nil {
		return ManagedNode{}, err
	}
	p, err := newProfile(s.Root, used, "", []string{protocol}, ranges...)
	if err != nil {
		return ManagedNode{}, err
	}
	for _, n := range MakeNodes(p, "") {
		if n.Protocol == protocol {
			n.ID = id
			return n, nil
		}
	}
	return ManagedNode{}, errors.New("preset unavailable")
}

func (s *PlatformService) PrepareCountry(ctx context.Context, c PlatformConfig, cc string) (PlatformConfig, error) {
	c.normalize()
	if err := c.Validate(); err != nil {
		return c, err
	}
	if !countryPattern.MatchString(cc) {
		return c, errors.New("country must be two uppercase letters")
	}
	if _, ok := c.VPN.Profiles[cc]; !ok {
		used, ranges, e := allocationPorts(s.Root, c.App, c.VPN)
		if e != nil {
			return c, e
		}
		p, e := NewProfile(s.Root, used, cc+"-", ranges...)
		if e != nil {
			return c, e
		}
		secret := p.Secrets
		secret.SocksUser, secret.SocksPass = "ygvg-"+cc, hexSecret()
		p.Secrets = Secrets{}
		c.VPN.Profiles[cc] = p
		for _, asset := range initialCertificates(App{Nodes: MakeNodes(p, "vpngate-"+cc+"-")}) {
			c.Certificates = append(c.Certificates, asset)
		}
		c.VPNSecrets[cc] = secret
	}
	c.VPN.ActiveCountry = cc
	p := c.VPNSecrets[cc]
	c.Gateway = merge(c.Gateway, Object{"country": cc, "enabled": true, "mode": "sticky", "fixed_host": "", "listen": fmt.Sprintf("127.0.0.1:%d", c.VPN.ProxyPort), "proxy_user": p.SocksUser, "proxy_pass": p.SocksPass, "data_dir": "/var/lib/yg-vpngate", "run_dir": "/run/yg-vpngate"})
	return c, nil
}
