package subscription

import "strings"

func egernNode(n Node) Object {
	kind := n.Protocol
	if kind == "ss" {
		kind = "shadowsocks"
	}
	m := Object{"name": n.Name, "server": n.Server, "port": n.Port}
	if n.Protocol != "tuic" {
		m["udp_relay"] = true
	}
	if n.Password != "" {
		m["password"] = n.Password
	}
	if n.TLS.Enabled {
		m["sni"], m["skip_tls_verify"] = n.TLS.ServerName, n.TLS.Insecure
		if n.TLS.CertificateHash != "" {
			m["fingerprint_sha256"] = n.TLS.CertificateHash
		}
	}
	switch n.Protocol {
	case "vmess", "vless":
		m["user_id"] = n.UUID
		if n.Protocol == "vmess" {
			m["security"] = fallback(n.Cipher, "auto")
		}
		if n.Flow != "" {
			m["flow"] = n.Flow
		}
		tls := Object{"sni": n.TLS.ServerName, "skip_tls_verify": n.TLS.Insecure}
		if n.TLS.CertificateHash != "" {
			tls["fingerprint_sha256"] = n.TLS.CertificateHash
		}
		if n.TLS.RealityKey != "" {
			tls["reality"] = Object{"public_key": n.TLS.RealityKey, "short_id": n.TLS.RealityShortID}
		}
		if n.Transport.Type == "ws" {
			tls["path"], tls["headers"] = n.Transport.Path, Object{"Host": n.Transport.Host}
			k := "ws"
			if n.TLS.Enabled {
				k = "wss"
			}
			m["transport"] = Object{k: tls}
		} else if n.Transport.Type == "grpc" {
			tls["service_name"] = n.Transport.ServiceName
			m["transport"] = Object{"grpc": tls}
		} else if n.TLS.Enabled {
			m["transport"] = Object{"tls": tls}
		}
		delete(m, "sni")
		delete(m, "skip_tls_verify")
		delete(m, "fingerprint_sha256")
	case "hysteria2":
		m["auth"] = n.Password
		delete(m, "password")
		if len(n.Ports) > 0 {
			m["port_hopping"] = strings.ReplaceAll(strings.Join(n.Ports, ","), ":", "-")
		}
		if n.HopIntervalSeconds != 0 {
			m["port_hopping_interval"] = n.HopIntervalSeconds
		}
		if n.Obfs != "" {
			m["obfs"], m["obfs_password"] = n.Obfs, n.ObfsPassword
		}
	case "tuic":
		m["uuid"], m["alpn"] = n.UUID, n.TLS.ALPN
		if len(n.TLS.ALPN) == 0 {
			m["alpn"] = []string{"h3"}
		}
	case "ss":
		m["method"] = n.Cipher
		if n.Cipher == "chacha20-ietf-poly1305" {
			m["method"] = "chacha20-poly1305"
		}
	case "trojan":
		if n.Transport.Type == "ws" {
			m["websocket"] = Object{"path": n.Transport.Path, "host": n.Transport.Host}
		}
	}
	return Object{kind: m}
}

func egernTemplate(nodes []Node, native []Object, t Template) Object {
	normal, gateway := groups(nodes)
	policy := []Object{}
	if len(normal) > 0 {
		policy = append(policy, Object{"select": Object{"name": "PROXY", "policies": normal}})
	}
	if len(gateway) > 0 {
		policy = append(policy, Object{"select": Object{"name": "VPNGate", "policies": gateway}})
	}
	rules := []Object{}
	for _, r := range t.Rules {
		target := r.Target
		if target == "PROXY" {
			target = templateFinal(nodes, t)
		}
		key := map[string]string{"domain": "domain", "domain_suffix": "domain_suffix", "ip_cidr": "ip_cidr"}[r.Type]
		if r.Type == "ip_cidr" {
			key = egernCIDRType(r.Value)
		}
		rules = append(rules, Object{key: Object{"match": r.Value, "policy": target}})
	}
	if t.PrivateNetwork {
		for _, cidr := range privateCIDRs {
			rules = append(rules, Object{egernCIDRType(cidr): Object{"match": cidr, "policy": "DIRECT"}})
		}
	}
	if t.Preset == "split" {
		rules = append(rules, Object{"geoip": Object{"match": "CN", "policy": "DIRECT"}})
	}
	rules = append(rules, Object{"default": Object{"policy": templateFinal(nodes, t)}})
	return applyNativeTemplate(t.Native, Object{"proxies": native, "policy_groups": policy, "rules": rules})
}

func egernCIDRType(cidr string) string {
	if strings.Contains(cidr, ":") {
		return "ip_cidr6"
	}
	return "ip_cidr"
}
