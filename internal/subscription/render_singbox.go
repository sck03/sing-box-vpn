package subscription

import "strings"

func (n Node) SingBox() Object {
	kind := n.Protocol
	if kind == "ss" {
		kind = "shadowsocks"
	}
	o := Object{"type": kind, "tag": n.Name, "server": n.Server, "server_port": n.Port}
	if n.UUID != "" {
		o["uuid"] = n.UUID
	}
	if n.Password != "" {
		o["password"] = n.Password
	}
	if n.Flow != "" {
		o["flow"] = n.Flow
	}
	if n.Protocol == "vmess" {
		o["security"] = "auto"
		if n.Cipher != "" {
			o["security"] = n.Cipher
		}
	}
	if n.Protocol == "ss" {
		o["method"] = n.Cipher
		if n.Plugin != nil {
			name := n.Plugin.Type
			if name == "obfs" {
				name = "obfs-local"
			}
			o["plugin"], o["plugin_opts"] = name, n.Plugin.options()
		}
	}
	if n.TLS.Enabled {
		t := Object{"enabled": true, "server_name": n.TLS.ServerName}
		if n.TLS.Insecure {
			t["insecure"] = true
		}
		if len(n.TLS.ALPN) > 0 {
			t["alpn"] = n.TLS.ALPN
		}
		if len(n.TLS.PublicKeyPins) > 0 {
			t["certificate_public_key_sha256"] = n.TLS.PublicKeyPins
			delete(t, "insecure")
		}
		if n.TLS.Fingerprint != "" {
			t["utls"] = Object{"enabled": true, "fingerprint": n.TLS.Fingerprint}
		}
		if n.TLS.RealityKey != "" {
			t["reality"] = Object{"enabled": true, "public_key": n.TLS.RealityKey, "short_id": n.TLS.RealityShortID}
		}
		o["tls"] = t
	}
	if n.Transport.Type == "ws" {
		o["transport"] = Object{"type": "ws", "path": n.Transport.Path, "headers": Object{"Host": n.Transport.Host}}
		transport := o["transport"].(Object)
		if n.Transport.EarlyData > 0 {
			transport["max_early_data"] = n.Transport.EarlyData
		}
		if n.Transport.EarlyDataHeader != "" {
			transport["early_data_header_name"] = n.Transport.EarlyDataHeader
		}
	}
	if n.Transport.Type == "grpc" {
		o["transport"] = Object{"type": "grpc", "service_name": n.Transport.ServiceName}
	}
	if n.Transport.Type == "httpupgrade" {
		o["transport"] = Object{"type": "httpupgrade", "host": n.Transport.Host, "path": n.Transport.Path}
	}
	if n.Obfs != "" {
		o["obfs"] = Object{"type": n.Obfs, "password": n.ObfsPassword}
	}
	if len(n.Ports) > 0 {
		ports := make([]string, len(n.Ports))
		for i, p := range n.Ports {
			p = strings.ReplaceAll(p, "-", ":")
			if !strings.Contains(p, ":") {
				p += ":" + p
			}
			ports[i] = p
		}
		o["server_ports"] = ports
	}
	if n.Protocol == "tuic" {
		o["congestion_control"], o["udp_relay_mode"], o["zero_rtt_handshake"], o["heartbeat"] = fallback(n.Congestion, "bbr"), fallback(n.UDPRelay, "native"), false, "10s"
	}
	if n.Protocol == "anytls" {
		o["idle_session_check_interval"], o["idle_session_timeout"], o["min_idle_session"] = "30s", "30s", 1
	}
	return o
}
