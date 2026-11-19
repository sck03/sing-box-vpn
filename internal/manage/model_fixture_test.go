package manage

import "github.com/sck03/sing-box-vpn/internal/domain"

// Fixtures can construct partial core objects for boundary tests. Production
// code never reconstructs authoritative nodes from dynamic core projections.
func fixtureNodes(inbounds []Object) []ManagedNode {
	nodes := make([]ManagedNode, 0, len(inbounds))
	for _, in := range inbounds {
		kind := text(in["type"])
		users := objects(in["users"])
		if len(users) > 1 {
			panic("fixture requires one user per node")
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
		n := ManagedNode{ID: text(in["tag"]), Protocol: kind, Port: number(in["listen_port"]), Flow: text(in["_managed_flow"]), TLS: ManagedTLS{Enabled: t["enabled"] == true, ServerName: text(t["server_name"]), Certificate: text(t["certificate_path"]), Key: text(t["key_path"]), RealityPrivate: text(r["private_key"]), RealityShortID: sid, HandshakeHost: text(hs["server"]), HandshakePort: number(hs["server_port"])}}
		n.Disabled = in["_disabled"] == true

		if kind == "vmess" {
			transport := object(in["transport"])
			n.Transport = ManagedTransport{Type: text(transport["type"]), Path: text(transport["path"]), Host: text(object(transport["headers"])["Host"]), EarlyData: number(transport["max_early_data"]), EarlyDataHeader: text(transport["early_data_header_name"])}
		}
		nodes = append(nodes, n)
	}
	return nodes
}

func (a App) fixtureProfile() Profile {
	p := a.profile()
	for _, n := range a.Nodes {
		if n.Protocol == "vless" {
			p.RealitySNI = n.TLS.ServerName
			p.HandshakeHost = n.TLS.HandshakeHost
			p.HandshakePort = n.TLS.HandshakePort
			p.Secrets.Reality = Reality{Private: n.TLS.RealityPrivate, ShortID: n.TLS.RealityShortID}
		}
		if n.Protocol == "vmess" {
			p.Certificate = n.TLS.Certificate
			p.Key = n.TLS.Key
			p.TLSSNI = n.TLS.ServerName
			p.VMessTLS = n.TLS.Enabled
			p.WSPath = n.Transport.Path
		}
	}
	return p
}
func fixtureApp(p Profile) App {
	return App{Nodes: MakeNodes(p, ""), Country: p.Country, Hopping: p.Hopping, TUICPorts: p.TUICPorts}
}

func fixtureCredential(kind string) domain.Credential {
	c, err := (domain.Credential{ID: "fixture", AccountID: "fixture", RuntimeID: "normal", InboundID: "in", Protocol: kind, Generation: 1, UUID: "01234567-89ab-4cde-8f01-23456789abcd", Password: "fixture-independent-password"}).Normalize()
	if err != nil {
		panic(err)
	}
	return c
}
