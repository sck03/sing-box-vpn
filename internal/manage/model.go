package manage

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// ManagedNode is the authoritative on-disk server node. Profile and sing-box's
// dynamic objects are reconstructed at the management/core boundaries only.
type ManagedNode struct {
	ID        string        `json:"id"`
	Protocol  string        `json:"protocol"`
	Port      int           `json:"port"`
	UUID      string        `json:"uuid,omitempty"`
	Password  string        `json:"password,omitempty"`
	Flow      string        `json:"flow,omitempty"`
	TLS       ManagedTLS    `json:"tls"`
	Transport sub.Transport `json:"transport,omitempty"`
}

type ManagedTLS struct {
	Enabled        bool   `json:"enabled"`
	ServerName     string `json:"server_name,omitempty"`
	Certificate    string `json:"certificate,omitempty"`
	Key            string `json:"key,omitempty"`
	RealityPrivate string `json:"reality_private,omitempty"`
	RealityShortID string `json:"reality_short_id,omitempty"`
	HandshakeHost  string `json:"handshake_host,omitempty"`
	HandshakePort  int    `json:"handshake_port,omitempty"`
}

type appDocument struct {
	Schema     int                 `json:"schema"`
	Version    string              `json:"version"`
	Name       string              `json:"name"`
	Address    string              `json:"address"`
	Nodes      []ManagedNode       `json:"nodes"`
	Country    string              `json:"country,omitempty"`
	Hopping    []string            `json:"hopping,omitempty"`
	TUICPorts  []string            `json:"tuic_ports,omitempty"`
	Routing    map[string][]string `json:"routing"`
	Warp       Object              `json:"warp"`
	SocksPort  int                 `json:"socks_port"`
	CDNAddress string              `json:"cdn_address,omitempty"`
	Argo       Object              `json:"argo,omitempty"`
}

func managedNodes(app App) ([]ManagedNode, error) {
	nodes := make([]ManagedNode, 0, len(app.Inbounds))
	for _, in := range app.Inbounds {
		kind := text(in["type"])
		users := objects(in["users"])
		if len(users) > 1 {
			return nil, errors.New("managed node requires exactly one user")
		}
		if len(users) == 0 {
			users = []Object{{}}
		}
		t := object(in["tls"])
		r := object(t["reality"])
		hs := object(r["handshake"])
		short := stringsValue(r["short_id"])
		sid := ""
		if len(short) > 0 {
			sid = short[0]
		}
		n := ManagedNode{ID: text(in["tag"]), Protocol: kind, Port: number(in["listen_port"]), UUID: text(users[0]["uuid"]), Password: text(users[0]["password"]), Flow: text(users[0]["flow"]), TLS: ManagedTLS{Enabled: t["enabled"] == true, ServerName: text(t["server_name"]), Certificate: text(t["certificate_path"]), Key: text(t["key_path"]), RealityPrivate: text(r["private_key"]), RealityShortID: sid, HandshakeHost: text(hs["server"]), HandshakePort: number(hs["server_port"])}}
		if n.TLS.ServerName == "" {
			if kind == "vless" {
				n.TLS.ServerName = app.Profile.RealitySNI
			} else {
				n.TLS.ServerName = app.Profile.TLSSNI
			}
		}
		if kind == "vmess" {
			transport := object(in["transport"])
			n.Transport = sub.Transport{Type: text(transport["type"]), Path: text(transport["path"]), Host: webSocketHost(transport, n.TLS.ServerName)}
			if n.Transport.Type == "ws" {
				if err := validateWebSocketPath(n.Transport.Path); err != nil {
					return nil, err
				}
			}
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

func (n ManagedNode) inbound() Object {
	in := Object{"type": n.Protocol, "tag": n.ID, "listen": "::", "listen_port": n.Port}
	user := Object{}
	if n.UUID != "" {
		user["uuid"] = n.UUID
	}
	if n.Password != "" {
		user["password"] = n.Password
	}
	if n.Flow != "" {
		user["flow"] = n.Flow
	}
	in["users"] = []Object{user}
	t := Object{"enabled": n.TLS.Enabled, "server_name": n.TLS.ServerName}
	in["tls"] = t
	if n.TLS.RealityPrivate != "" {
		t["reality"] = Object{"enabled": true, "private_key": n.TLS.RealityPrivate, "short_id": []string{n.TLS.RealityShortID}, "handshake": Object{"server": n.TLS.HandshakeHost, "server_port": n.TLS.HandshakePort}}
	} else {
		t["certificate_path"], t["key_path"] = n.TLS.Certificate, n.TLS.Key
	}
	if n.Protocol == "vmess" {
		user["alterId"] = 0
		in["transport"] = Object{"type": "ws", "path": n.Transport.Path, "headers": Object{"Host": n.Transport.Host}, "max_early_data": 2048, "early_data_header_name": "Sec-WebSocket-Protocol"}
	}
	if n.Protocol == "hysteria2" || n.Protocol == "tuic" {
		t["alpn"] = []string{"h3"}
	}
	if n.Protocol == "hysteria2" {
		in["ignore_client_bandwidth"] = false
	}
	if n.Protocol == "tuic" {
		in["congestion_control"] = "bbr"
	}
	return in
}

func (n ManagedNode) subscriptionNode(address, name, scope string, ports []string, cache certificateCache) (sub.Node, error) {
	node := sub.Node{ID: scope + "-" + n.Protocol, SourceID: scope, Name: name, Gateway: scope == vpngateScope, Protocol: n.Protocol, Server: address, Port: n.Port, UUID: n.UUID, Password: n.Password, Flow: n.Flow, Transport: n.Transport, TLS: sub.TLS{Enabled: n.TLS.Enabled, ServerName: n.TLS.ServerName}}
	if n.Protocol == "vless" {
		key, err := base64.RawURLEncoding.DecodeString(n.TLS.RealityPrivate)
		if err != nil {
			return node, err
		}
		private, err := ecdh.X25519().NewPrivateKey(key)
		if err != nil {
			return node, err
		}
		node.TLS.RealityKey = base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes())
		node.TLS.RealityShortID = n.TLS.RealityShortID
		node.TLS.Fingerprint = "chrome"
	} else if n.TLS.Enabled {
		info, err := cache.get(n.TLS.Certificate)
		if err != nil {
			return node, err
		}
		if info.SelfSigned {
			node.TLS.PublicKeyPins = []string{info.Pin}
			node.TLS.CertificateHash = info.Hash
			node.TLS.Insecure = true
		}
	}
	if n.Protocol == "hysteria2" {
		node.Ports = ports
		node.TLS.ALPN = []string{"h3"}
	}
	if n.Protocol == "tuic" {
		node.Congestion, node.UDPRelay = "bbr", "native"
		node.TLS.ALPN = []string{"h3"}
	}
	if n.Protocol == "vmess" {
		node.Cipher = "auto"
	}
	return node, node.Validate()
}

func (a App) MarshalJSON() ([]byte, error) {
	nodes, err := managedNodes(a)
	if err != nil {
		return nil, err
	}
	return json.Marshal(appDocument{Schema: 1, Version: a.Version, Name: a.Name, Address: a.Address, Nodes: nodes, Country: a.Profile.Country, Hopping: a.Profile.Hopping, TUICPorts: a.Profile.TUICPorts, Routing: a.Routing, Warp: a.Warp, SocksPort: a.SocksPort, CDNAddress: a.CDNAddress, Argo: a.Argo})
}

func (a *App) UnmarshalJSON(data []byte) error {
	var d appDocument
	if err := strictSubscriptionJSON(data, &d); err != nil {
		return err
	}
	if d.Schema != 1 {
		return errors.New("unsupported app schema; install into a new configuration directory")
	}
	app := App{Version: d.Version, Name: d.Name, Address: d.Address, Routing: d.Routing, Warp: d.Warp, SocksPort: d.SocksPort, CDNAddress: d.CDNAddress, Argo: d.Argo}
	p := Profile{Ports: map[string]int{}, Secrets: Secrets{UUID: map[string]string{}, Passwords: map[string]string{}}, Country: d.Country, Hopping: d.Hopping, TUICPorts: d.TUICPorts}
	for _, n := range d.Nodes {
		if Labels[n.Protocol] == "" || p.Ports[n.Protocol] != 0 || n.Port < 1 || n.Port > 65535 {
			return errors.New("invalid or duplicate managed node")
		}
		p.Ports[n.Protocol] = n.Port
		if n.UUID != "" {
			p.Secrets.UUID[n.Protocol] = n.UUID
		}
		if n.Password != "" {
			p.Secrets.Passwords[n.Protocol] = n.Password
		}
		if n.Protocol == "vless" {
			key, err := base64.RawURLEncoding.DecodeString(n.TLS.RealityPrivate)
			if err != nil {
				return err
			}
			private, err := ecdh.X25519().NewPrivateKey(key)
			if err != nil {
				return err
			}
			p.Secrets.Reality = Reality{Private: n.TLS.RealityPrivate, Public: base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()), ShortID: n.TLS.RealityShortID}
			p.RealitySNI, p.HandshakeHost, p.HandshakePort = n.TLS.ServerName, n.TLS.HandshakeHost, n.TLS.HandshakePort
		}
		if n.Protocol == "vmess" {
			p.Certificate, p.Key, p.TLSSNI, p.VMessTLS, p.WSPath = n.TLS.Certificate, n.TLS.Key, n.TLS.ServerName, n.TLS.Enabled, n.Transport.Path
		}
		app.Inbounds = append(app.Inbounds, n.inbound())
	}
	app.Profile = p
	*a = app
	return nil
}
