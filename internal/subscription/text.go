package subscription

import (
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
)

func textTemplate(nodes []Node, t Template) ([]byte, error) {
	var b strings.Builder
	if t.Mode == "config" {
		if t.Client == "quantumult-x" {
			b.WriteString("[general]\n\n[dns]\n")
			for _, dns := range templateDNS(t) {
				fmt.Fprintf(&b, "server=%s\n", dns)
			}
			b.WriteString("\n[server_local]\n")
		} else {
			fmt.Fprintf(&b, "[General]\ndns-server = %s\n\n[Proxy]\n", strings.Join(templateDNS(t), ","))
		}
	}
	for _, n := range nodes {
		var line string
		switch t.Client {
		case "loon":
			line = loonNode(n)
		case "quantumult-x":
			line = quantumultNode(n)
		default:
			line = surgeNode(n)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if t.Mode != "config" {
		return []byte(b.String()), nil
	}
	normal, gateway := groups(nodes)
	if t.Client == "quantumult-x" {
		b.WriteString("\n[policy]\n")
		if len(normal) > 0 {
			b.WriteString("static=PROXY," + strings.Join(normal, ",") + "\n")
		}
		if len(gateway) > 0 {
			b.WriteString("static=VPNGate," + strings.Join(gateway, ",") + "\n")
		}
		b.WriteString("\n[filter_local]\n")
		for _, r := range t.Rules {
			kind := map[string]string{"domain": "host", "domain_suffix": "host-suffix", "ip_cidr": "ip-cidr"}[r.Type]
			if r.Type == "ip_cidr" && strings.Contains(r.Value, ":") {
				kind = "ip6-cidr"
			}
			target := r.Target
			if target == "PROXY" {
				target = finalGroup(nodes)
			}
			fmt.Fprintf(&b, "%s,%s,%s\n", kind, r.Value, target)
		}
		if t.Preset == "split" {
			b.WriteString("geoip,CN,DIRECT\n")
		}
		b.WriteString("final," + finalGroup(nodes) + "\n")
	} else {
		b.WriteString("\n[Proxy Group]\n")
		if len(normal) > 0 {
			b.WriteString("PROXY = select," + strings.Join(normal, ",") + "\n")
		}
		if len(gateway) > 0 {
			b.WriteString("VPNGate = select," + strings.Join(gateway, ",") + "\n")
		}
		b.WriteString("\n[Rule]\n")
		for _, r := range t.Rules {
			target := r.Target
			if target == "PROXY" {
				target = finalGroup(nodes)
			}
			b.WriteString(textRule(r, target) + "\n")
		}
		if t.Preset == "split" {
			b.WriteString("GEOIP,CN,DIRECT\n")
		}
		b.WriteString("FINAL," + finalGroup(nodes) + "\n")
	}
	return []byte(b.String()), nil
}

func surgeNode(n Node) string {
	kind := n.Protocol
	if kind == "tuic" {
		kind = "tuic-v5"
	}
	parts := []string{n.Name + " = " + kind, n.Server, strconv.Itoa(n.Port)}
	if n.UUID != "" {
		key := "username"
		if n.Protocol == "tuic" {
			key = "uuid"
		}
		parts = append(parts, key+"="+n.UUID)
	}
	if n.Password != "" {
		parts = append(parts, `password="`+n.Password+`"`)
	}
	if n.Protocol == "vmess" {
		parts = append(parts, "vmess-aead=true", "encrypt-method="+fallback(n.Cipher, "auto"))
	}
	if n.Protocol == "ss" {
		parts = append(parts, "encrypt-method="+n.Cipher)
	}
	if n.Flow != "" {
		parts = append(parts, "flow="+n.Flow)
	}
	if n.TLS.Enabled {
		if n.Protocol == "vmess" || n.Protocol == "vless" {
			parts = append(parts, "tls=true")
		}
		parts = append(parts, "sni="+n.TLS.ServerName, "skip-cert-verify="+strconv.FormatBool(n.TLS.Insecure))
		if n.TLS.CertificateHash != "" {
			parts = append(parts, "server-cert-fingerprint-sha256="+n.TLS.CertificateHash)
		}
		if len(n.TLS.ALPN) > 0 {
			parts = append(parts, `alpn="`+strings.Join(n.TLS.ALPN, ",")+`"`)
		}
		if n.TLS.RealityKey != "" {
			parts = append(parts, "reality-public-key="+n.TLS.RealityKey, "reality-short-id="+n.TLS.RealityShortID)
		}
	}
	if n.Transport.Type == "ws" {
		parts = append(parts, "ws=true", "ws-path="+n.Transport.Path, `ws-headers="Host:`+n.Transport.Host+`"`)
	}
	if len(n.Ports) > 0 {
		parts = append(parts, `port-hopping="`+strings.ReplaceAll(strings.Join(n.Ports, ";"), ":", "-")+`"`)
	}
	parts = append(parts, "udp-relay=true")
	return strings.Join(parts, ",")
}

func loonNode(n Node) string {
	kind := n.Protocol
	if kind == "hysteria2" {
		kind = "Hysteria2"
	}
	if kind == "ss" {
		kind = "shadowsocks"
	}
	parts := []string{n.Name + " = " + kind, n.Server, strconv.Itoa(n.Port)}
	if n.Protocol == "vmess" {
		parts = append(parts, fallback(n.Cipher, "auto"))
	}
	if n.Protocol == "ss" {
		parts = append(parts, n.Cipher)
	}
	credential := n.Password
	if n.UUID != "" {
		credential = n.UUID
	}
	parts = append(parts, `"`+credential+`"`)
	if n.Protocol == "vmess" || n.Protocol == "vless" {
		parts = append(parts, "transport="+fallback(n.Transport.Type, "tcp"), "over-tls="+strconv.FormatBool(n.TLS.Enabled))
	}
	if n.Transport.Type == "ws" {
		parts = append(parts, "path="+n.Transport.Path, "host="+n.Transport.Host)
	}
	if n.Flow != "" {
		parts = append(parts, "flow="+n.Flow)
	}
	if n.TLS.Enabled {
		parts = append(parts, "tls-name="+n.TLS.ServerName, "skip-cert-verify="+strconv.FormatBool(n.TLS.Insecure))
	}
	if n.TLS.RealityKey != "" {
		parts = append(parts, "public-key="+n.TLS.RealityKey, "short-id="+n.TLS.RealityShortID, "sni="+n.TLS.ServerName)
	}
	if len(n.TLS.ALPN) > 0 {
		parts = append(parts, `alpn="`+strings.Join(n.TLS.ALPN, ",")+`"`)
	}
	parts = append(parts, "udp=true")
	return strings.Join(parts, ",")
}

func quantumultNode(n Node) string {
	kind := n.Protocol
	if kind == "ss" {
		kind = "shadowsocks"
	}
	parts := []string{kind + "=" + net.JoinHostPort(n.Server, strconv.Itoa(n.Port))}
	credential := n.Password
	if n.UUID != "" {
		credential = n.UUID
	}
	parts = append(parts, "password="+credential)
	if n.Protocol == "vmess" {
		parts = append(parts, "method="+fallback(n.Cipher, "chacha20-ietf-poly1305"), "aead=true")
	}
	if n.Protocol == "vless" {
		parts = append(parts, "method=none")
	}
	if n.Protocol == "ss" {
		parts = append(parts, "method="+n.Cipher)
	}
	if n.Transport.Type == "ws" {
		mode := "ws"
		if n.TLS.Enabled {
			mode = "wss"
		}
		parts = append(parts, "obfs="+mode, "obfs-host="+n.Transport.Host, "obfs-uri="+n.Transport.Path)
	} else if n.TLS.Enabled {
		if n.Protocol == "vmess" || n.Protocol == "vless" {
			parts = append(parts, "obfs=over-tls")
		} else {
			parts = append(parts, "over-tls=true")
		}
	}
	if n.TLS.Enabled {
		parts = append(parts, "tls-host="+n.TLS.ServerName, "tls-verification="+strconv.FormatBool(!n.TLS.Insecure))
	}
	if n.TLS.RealityKey != "" {
		parts = append(parts, "reality-base64-pubkey="+n.TLS.RealityKey, "reality-hex-shortid="+n.TLS.RealityShortID)
	}
	if n.Flow != "" {
		parts = append(parts, "vless-flow="+n.Flow)
	}
	if len(n.TLS.ALPN) > 0 {
		var data []byte
		for _, v := range n.TLS.ALPN {
			data = append(data, byte(len(v)))
			data = append(data, []byte(v)...)
		}
		parts = append(parts, "tls-alpn="+hex.EncodeToString(data))
	}
	parts = append(parts, "udp-relay=true", "tag="+n.Name)
	return strings.Join(parts, ",")
}
