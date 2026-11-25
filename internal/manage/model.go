package manage

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/sck03/sing-box-vpn/internal/domain"
	"github.com/sck03/sing-box-vpn/internal/serverconfig"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

const AppSchema = 5

type ManagedNode serverconfig.Node
type ManagedTLS = serverconfig.TLS
type ManagedTransport = serverconfig.Transport

func (n ManagedNode) inbound() Object { return serverconfig.Node(n).Inbound() }
func (n ManagedNode) Validate() error { return serverconfig.Node(n).Validate() }

func managedNodes(app App) ([]ManagedNode, error) {
	if len(app.Nodes) > 256 {
		return nil, errors.New("at most 256 inbounds")
	}
	return app.Nodes, nil
}

// Inbounds returns a detached core projection, never the authoritative state.
func (a App) Inbounds() []Object {
	inbounds := make([]Object, 0, len(a.Nodes))
	for _, n := range a.Nodes {
		inbounds = append(inbounds, n.inbound())
	}
	return inbounds
}

// profile supplies port allocation metadata for the shared VPNGate allocator.
// Credentials and TLS settings are never reconstructed from this view.
func (a App) profile() Profile {
	p := Profile{Ports: map[string]int{}, Hopping: a.Hopping, TUICPorts: a.TUICPorts}
	for _, n := range a.Nodes {
		if p.Ports[n.Protocol] == 0 {
			p.Ports[n.Protocol] = n.Port
		}
	}
	return p
}

func (a App) MarshalJSON() ([]byte, error) {
	type document App
	return json.Marshal(struct {
		Schema int `json:"schema"`
		document
	}{AppSchema, document(a)})
}
func (a *App) UnmarshalJSON(data []byte) error {
	type document App
	var d struct {
		Schema int `json:"schema"`
		document
	}
	if err := strictSubscriptionJSON(data, &d); err != nil {
		return err
	}
	if d.Schema != AppSchema {
		return errors.New("unsupported app schema; use a fresh configuration directory")
	}
	*a = App(d.document)
	return nil
}
func (n ManagedNode) subscriptionNode(address, name, scope string, ports []string, cache certificateCache, credential domain.Credential) (sub.Node, error) {
	node := sub.Node{ID: scope + "-" + n.Protocol, SourceID: scope, Name: name, Gateway: scope == vpngateScope, Protocol: n.Protocol, Server: address, Port: n.Port, UUID: credential.UUID, Password: credential.Password, Flow: n.Flow, Transport: sub.Transport{Type: n.Transport.Type, Path: n.Transport.Path, Host: n.Transport.Host, EarlyData: n.Transport.EarlyData, EarlyDataHeader: n.Transport.EarlyDataHeader}, TLS: sub.TLS{Enabled: n.TLS.Enabled, ServerName: n.TLS.ServerName}}
	if n.ID != Tags[n.Protocol] && !strings.HasPrefix(n.ID, "vg-") {
		node.ID = scope + "-" + n.ID
	}
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
