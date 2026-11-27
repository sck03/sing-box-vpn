package manage

import (
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"slices"
)

// Candidate preparation uses only explicit configuration and local port/material
// services. It never reads or changes the database's desired configuration.
func prepareInbound(root string, c PlatformConfig, protocol, id string) (ManagedNode, error) {
	if !domain.ResourceID.MatchString(id) || !slices.Contains(Protocols, protocol) {
		return ManagedNode{}, errors.New("invalid preset")
	}
	c.normalize()
	if err := c.Validate(); err != nil {
		return ManagedNode{}, err
	}
	if slices.ContainsFunc(c.App.Nodes, func(n ManagedNode) bool { return n.ID == id }) {
		return ManagedNode{}, &domain.FieldError{Field: "id", Message: "入站 ID 已存在"}
	}
	used, ranges, err := allocationPorts(root, c.App, c.VPN, c.Listener)
	if err != nil {
		return ManagedNode{}, err
	}
	p, err := newProfile(root, used, "", []string{protocol}, ranges...)
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

func prepareCountry(root string, c PlatformConfig, cc string) (PlatformConfig, error) {
	c.normalize()
	if err := c.Validate(); err != nil {
		return c, err
	}
	if !countryPattern.MatchString(cc) {
		return c, errors.New("country must be two uppercase letters")
	}
	if _, ok := c.VPN.Profiles[cc]; !ok {
		used, ranges, e := allocationPorts(root, c.App, c.VPN, c.Listener)
		if e != nil {
			return c, e
		}
		p, e := NewProfile(root, used, cc+"-", ranges...)
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
