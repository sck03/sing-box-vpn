package serverconfig

// Inbound returns a detached core projection of one managed node.
func (n Node) Inbound() map[string]any {
	in := map[string]any{"type": n.Protocol, "tag": n.ID, "listen": "::", "listen_port": n.Port}
	if n.Disabled {
		in["_disabled"] = true
	}
	// The listener has no authentication authority. Runtime binding supplies all
	// users from the account repository and removes this private policy field.
	in["users"] = []map[string]any{}
	if n.Flow != "" {
		in["_managed_flow"] = n.Flow
	}
	t := map[string]any{"enabled": n.TLS.Enabled, "server_name": n.TLS.ServerName}
	in["tls"] = t
	if n.TLS.RealityPrivate != "" {
		t["reality"] = map[string]any{"enabled": true, "private_key": n.TLS.RealityPrivate, "short_id": []string{n.TLS.RealityShortID}, "handshake": map[string]any{"server": n.TLS.HandshakeHost, "server_port": n.TLS.HandshakePort}}
	} else {
		t["certificate_path"], t["key_path"] = n.TLS.Certificate, n.TLS.Key
	}
	if n.Protocol == "vmess" {
		header := n.Transport.EarlyDataHeader
		// sing-box's empty-header path mode compares RequestURI, which is
		// percent-encoded for Unicode paths even when early data is disabled.
		// Choosing a header in that disabled case preserves ordinary WS semantics.
		if n.Transport.EarlyData == 0 && header == "" {
			header = "Sec-WebSocket-Protocol"
		}
		in["transport"] = map[string]any{"type": n.Transport.Type, "path": n.Transport.Path, "headers": map[string]any{"Host": n.Transport.Host}, "max_early_data": n.Transport.EarlyData, "early_data_header_name": header}
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
