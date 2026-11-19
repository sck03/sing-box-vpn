package subscription

import "strings"

func (n Node) Mihomo() Object {
	o := Object{"name": n.Name, "type": n.Protocol, "server": n.Server, "port": n.Port, "udp": true}
	if n.UUID != "" {
		o["uuid"] = n.UUID
	}
	if n.Password != "" {
		o["password"] = n.Password
	}
	if n.Cipher != "" {
		o["cipher"] = n.Cipher
	}
	if n.Plugin != nil {
		p := n.Plugin
		opts := Object{"mode": p.Mode}
		if p.Host != "" {
			opts["host"] = p.Host
		}
		if p.Path != "" {
			opts["path"] = p.Path
		}
		if p.TLS {
			opts["tls"] = true
		}
		if p.Mux {
			opts["mux"] = true
		}
		o["plugin"], o["plugin-opts"] = p.Type, opts
	}
	if n.Flow != "" {
		o["flow"] = n.Flow
	}
	if n.Protocol == "vless" || n.Protocol == "vmess" {
		o["network"], o["tls"], o["servername"] = fallback(n.Transport.Type, "tcp"), n.TLS.Enabled, n.TLS.ServerName
	}
	if n.Protocol == "trojan" && n.Transport.Type != "" {
		o["network"] = n.Transport.Type
	}
	if n.Protocol == "vmess" {
		o["cipher"], o["alterId"] = fallback(n.Cipher, "auto"), 0
	}
	if n.TLS.Enabled {
		if n.Protocol != "vless" && n.Protocol != "vmess" {
			o["sni"] = n.TLS.ServerName
		}
		o["skip-cert-verify"] = n.TLS.Insecure
		if n.TLS.CertificateHash != "" {
			o["fingerprint"] = n.TLS.CertificateHash
			o["skip-cert-verify"] = false
		}
		if len(n.TLS.ALPN) > 0 {
			o["alpn"] = n.TLS.ALPN
		}
		if n.TLS.Fingerprint != "" {
			o["client-fingerprint"] = n.TLS.Fingerprint
		}
		if n.TLS.RealityKey != "" {
			o["reality-opts"] = Object{"public-key": n.TLS.RealityKey, "short-id": n.TLS.RealityShortID}
		}
	}
	if n.Transport.Type == "ws" {
		o["ws-opts"] = Object{"path": n.Transport.Path, "headers": Object{"Host": n.Transport.Host}}
		options := o["ws-opts"].(Object)
		if n.Transport.EarlyData > 0 {
			options["max-early-data"] = n.Transport.EarlyData
		}
		if n.Transport.EarlyDataHeader != "" {
			options["early-data-header-name"] = n.Transport.EarlyDataHeader
		}
	}
	if n.Transport.Type == "grpc" {
		o["grpc-opts"] = Object{"grpc-service-name": n.Transport.ServiceName}
	}
	if n.Transport.Type == "httpupgrade" {
		o["network"] = "ws"
		o["ws-opts"] = Object{"path": n.Transport.Path, "headers": Object{"Host": n.Transport.Host}, "v2ray-http-upgrade": true}
	}
	if n.Transport.Type == "xhttp" {
		x := n.Transport.XHTTP.native()
		x["path"], x["host"], x["mode"] = n.Transport.Path, n.Transport.Host, fallback(n.Transport.Mode, "auto")
		o["xhttp-opts"] = x
	}
	if n.Obfs != "" {
		o["obfs"], o["obfs-password"] = n.Obfs, n.ObfsPassword
	}
	if len(n.Ports) > 0 {
		o["ports"] = strings.ReplaceAll(strings.Join(n.Ports, ","), ":", "-")
	}
	if n.HopIntervalSeconds != 0 {
		o["hop-interval"] = n.HopIntervalSeconds
	}
	if n.Protocol == "tuic" {
		o["congestion-controller"], o["udp-relay-mode"] = fallback(n.Congestion, "bbr"), fallback(n.UDPRelay, "native")
	}
	if n.Protocol == "anytls" {
		o["idle-session-check-interval"], o["idle-session-timeout"], o["min-idle-session"] = 30, 30, 1
	}
	return o
}
