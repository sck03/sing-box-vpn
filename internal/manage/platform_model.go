package manage

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/certificates"
	"github.com/sck03/sing-box-vpn/internal/domain"
	strict "github.com/sck03/sing-box-vpn/internal/encoding"
	"github.com/sck03/sing-box-vpn/internal/gatewayconfig"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"net"
	"slices"
	"strings"
)

const PlatformSchema = 6

// PlatformConfig is the single authoritative aggregate stored in control.db.
// JSON files consumed by the gateway and compiler are generated projections.
type PlatformConfig struct {
	Schema        int                  `json:"schema"`
	App           App                  `json:"app"`
	VPN           VPNSettings          `json:"vpn"`
	VPNSecrets    map[string]Secrets   `json:"vpn_secrets"`
	Gateway       Object               `json:"gateway"`
	Subscriptions sub.Config           `json:"subscriptions"`
	Listener      subscriptionSettings `json:"listener"`
	Certificates  []CertificateAsset   `json:"certificates"`
	Argo          TunnelSettings       `json:"tunnel"`
}
type CertificateAsset = certificates.CertificateAsset
type TunnelSettings struct {
	Mode    string `json:"mode"`
	Domain  string `json:"domain"`
	Token   string `json:"token"`
	Inbound string `json:"inbound"`
}

func (c PlatformConfig) Validate() error {
	if c.Schema != PlatformSchema {
		return fmt.Errorf("platform schema must be %d; old data cannot be imported", PlatformSchema)
	}
	if net.ParseIP(c.App.Address) == nil {
		if err := validateDomain(c.App.Address); err != nil {
			return err
		}
	}
	if len(c.App.Name) == 0 || len(c.App.Name) > 128 {
		return errors.New("server name must contain 1–128 bytes")
	}
	nodes, err := managedNodes(c.App)
	if err != nil {
		return err
	}
	ports, tags := map[int]bool{}, map[string]bool{}
	for _, n := range nodes {
		if err := n.Validate(); err != nil {
			return err
		}
		if !domain.ResourceID.MatchString(n.ID) || tags[n.ID] || !slices.Contains(Protocols, n.Protocol) {
			return errors.New("invalid or duplicate inbound ID/protocol")
		}
		tags[n.ID] = true
		if n.Port < 1 || n.Port > 65535 || ports[n.Port] {
			return errors.New("inbound ports must be unique and within 1–65535")
		}
		ports[n.Port] = true
	}
	if len(nodes) > 256 {
		return errors.New("at most 256 inbounds")
	}
	if c.VPN.ProxyPort < 1 || c.VPN.ProxyPort > 65535 || ports[c.VPN.ProxyPort] {
		return errors.New("invalid or conflicting gateway proxy port")
	}
	ports[c.VPN.ProxyPort] = true
	for cc, p := range c.VPN.Profiles {
		if !countryPattern.MatchString(cc) {
			return errors.New("invalid VPNGate country")
		}
		for _, port := range p.Ports {
			if port < 1 || port > 65535 || ports[port] {
				return errors.New("VPNGate ports conflict")
			}
			ports[port] = true
		}
	}
	if c.Listener.Enabled && (c.Listener.Port < 1 || c.Listener.Port > 65535 || ports[c.Listener.Port] || net.ParseIP(c.Listener.Listen) == nil) {
		return errors.New("invalid or conflicting download listener")
	}
	if c.Listener.Enabled {
		if err := c.Listener.Validate(); err != nil {
			return err
		}
	}
	if err = c.Subscriptions.Validate(); err != nil {
		return err
	}
	if err = validateServerResources(c.App.Advanced); err != nil {
		return err
	}
	if c.Argo.Mode != "" && !slices.Contains([]string{"off", "temporary", "fixed"}, c.Argo.Mode) {
		return errors.New("invalid Argo mode")
	}
	if c.Argo.Mode == "fixed" {
		if err = validateDomain(c.Argo.Domain); err != nil {
			return err
		}
		if len(c.Argo.Token) < 20 || len(c.Argo.Token) > 8192 || strings.ContainsAny(c.Argo.Token, "\r\n\x00") {
			return errors.New("invalid Argo token")
		}
		if !tunnelToken.MatchString(c.Argo.Token) {
			return errors.New("invalid Argo token characters")
		}
	}
	if c.Argo.Mode == "fixed" || c.Argo.Mode == "temporary" {
		if !slices.ContainsFunc(nodes, func(n ManagedNode) bool { return n.ID == c.Argo.Inbound && n.Protocol == "vmess" && !n.Disabled }) {
			return errors.New("Argo requires an enabled VMess inbound")
		}
	}
	if c.VPN.ActiveCountry != "" {
		secret, ok := c.VPNSecrets[c.VPN.ActiveCountry]
		if !ok {
			return errors.New("VPNGate is missing country credentials")
		}
		if text(c.Gateway["country"]) != c.VPN.ActiveCountry || text(c.Gateway["proxy_user"]) != secret.SocksUser || text(c.Gateway["proxy_pass"]) != secret.SocksPass || text(c.Gateway["listen"]) != fmt.Sprintf("127.0.0.1:%d", c.VPN.ProxyPort) {
			return errors.New("VPNGate identity fields are owned by country selection")
		}
	}
	if len(c.Gateway) > 0 || c.VPN.ActiveCountry != "" {
		raw, err := json.Marshal(c.Gateway)
		if err != nil {
			return err
		}
		gateway, err := gatewayconfig.Decode(raw)
		if err != nil {
			return err
		}
		if c.VPN.ActiveCountry != "" {
			err = gateway.Validate()
		} else {
			err = gateway.ValidatePolicy()
		}
		if err != nil {
			return fmt.Errorf("VPNGate: %w", err)
		}
	}
	if len(c.Certificates) > 256 {
		return errors.New("at most 256 certificate assets")
	}
	seen := map[string]bool{}
	for _, asset := range c.Certificates {
		if !domain.ResourceID.MatchString(asset.ID) || seen[asset.ID] {
			return errors.New("invalid certificate ID")
		}
		seen[asset.ID] = true
		if err = validateDomain(asset.Domain); err != nil {
			return err
		}
		if asset.Provider != "import" && asset.Provider != "generated" && !automaticCertificate(asset) {
			return errors.New("certificate provider must be import, generated or acme")
		}
	}
	gateway := Object{}
	if c.VPN.ActiveCountry != "" {
		gateway, err = vpnServer(c.VPN, c.VPNSecrets)
		if err != nil {
			return err
		}
	}
	if _, _, err := firewallRules(c.App, c.VPN, gateway, c.Listener); err != nil {
		return err
	}
	for _, ranges := range [][]string{c.App.Hopping, c.App.TUICPorts} {
		parsed, err := parsePortRanges(ranges)
		if err != nil {
			return err
		}
		for _, r := range parsed {
			for _, n := range nodes {
				if n.Port >= r.first && n.Port <= r.last {
					return errors.New("forwarding range overlaps an inbound listener")
				}
			}
		}
	}
	for _, service := range c.App.Advanced.Services {
		port := number(service["listen_port"])
		if port < 1 || port > 65535 || ports[port] || (c.Listener.Enabled && c.Listener.Port == port) {
			return errors.New("core service listener conflicts with a managed port")
		}
		ports[port] = true
	}
	return nil
}

func validateServerResources(c CoreSettings) error {
	if _, exists := c.DNS["independent_cache"]; exists {
		return errors.New("DNS independent_cache is obsolete; remove it from DNS settings")
	}
	if len(c.Outbounds) > 256 || len(c.Endpoints) > 32 || len(c.Services) > 16 || len(objects(c.Route["rules"])) > 1000 {
		return errors.New("server resource budget exceeded")
	}
	tags := map[string]Object{}
	for _, v := range append(append([]Object{}, c.Outbounds...), c.Endpoints...) {
		tag := text(v["tag"])
		if !domain.ResourceID.MatchString(tag) || tags[tag] != nil {
			return errors.New("outbound/endpoint tags must be unique")
		}
		tags[tag] = v
	}
	if c.Outbounds != nil {
		for _, v := range c.Outbounds {
			if !slices.Contains([]string{"direct", "socks", "selector", "urltest", "vless", "vmess", "hysteria2", "tuic", "anytls"}, text(v["type"])) {
				return errors.New("unsupported managed outbound type")
			}
		}
	}
	for _, v := range c.Endpoints {
		if v["type"] != "wireguard" {
			return errors.New("only WireGuard endpoints are registered")
		}
	}
	for _, v := range c.Services {
		if v["type"] != "resolved" {
			return errors.New("only the resolved DNS service is registered; unguarded proxy services are prohibited")
		}
		if listen := text(v["listen"]); listen != "127.0.0.1" && listen != "::1" {
			return errors.New("resolved service must listen on a loopback IP")
		}
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if done[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("outbound dependency cycle at %s", id)
		}
		v := tags[id]
		if v == nil {
			return fmt.Errorf("missing outbound %s", id)
		}
		visiting[id] = true
		refs := stringsValue(v["outbounds"])
		if d := text(v["detour"]); d != "" {
			refs = append(refs, d)
		}
		for _, ref := range refs {
			if err := visit(ref); err != nil {
				return err
			}
		}
		visiting[id] = false
		done[id] = true
		return nil
	}
	for id := range tags {
		if err := visit(id); err != nil {
			return err
		}
	}
	if c.Experimental != nil {
		for key := range c.Experimental {
			if key != "cache_file" {
				return fmt.Errorf("experimental %s is not managed", key)
			}
		}
	}
	if text(c.Log["output"]) != "" {
		return errors.New("logs are owned by journald; custom log paths are not accepted")
	}
	return nil
}

func serverForApp(app App) (Object, error) {
	nodes, err := managedNodes(app)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		if err := node.Validate(); err != nil {
			return nil, err
		}
	}
	active := []Object{}
	for _, in := range app.Inbounds() {
		if in["_disabled"] != true {
			active = append(active, in)
		}
	}
	routing := app.Routing
	if app.Advanced.Route != nil {
		routing = nil
	}
	server, err := ServerConfig(active, routing, app.Advanced.Endpoints, app.SocksPort)
	if err != nil {
		return nil, err
	}
	c := app.Advanced
	for name, value := range map[string]Object{"route": c.Route, "dns": c.DNS, "log": c.Log, "experimental": c.Experimental} {
		if value != nil {
			server[name] = value
		}
	}
	if c.Outbounds != nil {
		server["outbounds"] = c.Outbounds
	}
	if c.Endpoints != nil {
		server["endpoints"] = c.Endpoints
	}
	if c.Services != nil {
		server["services"] = c.Services
	}
	return server, nil
}

func decodePlatform(data []byte) (PlatformConfig, error) {
	var c PlatformConfig
	err := strict.JSON(data, &c, maxJSONFile)
	c.normalize()
	return c, err
}
func (c *PlatformConfig) normalize() {
	if c.VPN.Profiles == nil {
		c.VPN.Profiles = map[string]Profile{}
	}
	if c.VPNSecrets == nil {
		c.VPNSecrets = map[string]Secrets{}
	}
	if c.Gateway == nil {
		c.Gateway = Object{}
	}
}
func encodePlatform(c PlatformConfig) ([]byte, error) { return json.Marshal(c) }
