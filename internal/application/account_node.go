package application

import (
	"crypto/ecdh"
	"encoding/base64"

	"github.com/google/uuid"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"github.com/sck03/sing-box-vpn/internal/serverconfig"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// CertificateIdentity is the public material needed by client projections.
// File access and per-export caching belong to the platform adapter.
type CertificateIdentity struct {
	Pin, Hash  string
	SelfSigned bool
}

// AccountNode projects a bound inbound without retaining platform state. IDs
// depend only on the credential, stay bounded even for 64-byte resource IDs,
// and leave room for independently addressable variants such as Argo.
// The caller validates after applying endpoint and port overrides.
func AccountNode(n serverconfig.Node, address, name string, credential domain.Credential, certificate func(string) (CertificateIdentity, error)) (sub.Node, error) {
	scope := credential.RuntimeID
	id := scope + "-" + uuid.NewSHA1(uuid.NameSpaceOID, []byte(credential.ID)).String()
	node := sub.Node{ID: id, SourceID: scope, Name: name, Gateway: scope == "vpngate", Protocol: n.Protocol, Server: address, Port: n.Port, UUID: credential.UUID, Password: credential.Password, Flow: n.Flow, Transport: sub.Transport{Type: n.Transport.Type, Path: n.Transport.Path, Host: n.Transport.Host, EarlyData: n.Transport.EarlyData, EarlyDataHeader: n.Transport.EarlyDataHeader}, TLS: sub.TLS{Enabled: n.TLS.Enabled, ServerName: n.TLS.ServerName}}
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
		info, err := certificate(n.TLS.Certificate)
		if err != nil {
			return node, err
		}
		if info.SelfSigned {
			node.TLS.PublicKeyPins = []string{info.Pin}
			node.TLS.CertificateHash = info.Hash
			node.TLS.Insecure = true
		}
	}
	switch n.Protocol {
	case "hysteria2":
		node.Ports = append([]string(nil), n.UDPPorts...)
		node.TLS.ALPN = []string{"h3"}
	case "tuic":
		node.Congestion, node.UDPRelay = "bbr", "native"
		node.TLS.ALPN = []string{"h3"}
	case "vmess":
		node.Cipher = "auto"
	}
	return node, nil
}
