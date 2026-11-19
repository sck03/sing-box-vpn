package subscription

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Object = map[string]any

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
	if n.Flow != "" {
		o["flow"] = n.Flow
	}
	if n.Protocol == "vless" || n.Protocol == "vmess" {
		o["network"], o["tls"], o["servername"] = fallback(n.Transport.Type, "tcp"), n.TLS.Enabled, n.TLS.ServerName
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
	}
	if n.Transport.Type == "grpc" {
		o["grpc-opts"] = Object{"grpc-service-name": n.Transport.ServiceName}
	}
	if n.Transport.Type == "httpupgrade" {
		o["network"] = "ws"
		o["ws-opts"] = Object{"path": n.Transport.Path, "headers": Object{"Host": n.Transport.Host}, "v2ray-http-upgrade": true}
	}
	if n.Obfs != "" {
		o["obfs"], o["obfs-password"] = n.Obfs, n.ObfsPassword
	}
	if len(n.Ports) > 0 {
		o["ports"] = strings.ReplaceAll(strings.Join(n.Ports, ","), ":", "-")
	}
	if n.Protocol == "tuic" {
		o["congestion-controller"], o["udp-relay-mode"] = fallback(n.Congestion, "bbr"), fallback(n.UDPRelay, "native")
	}
	if n.Protocol == "anytls" {
		o["idle-session-check-interval"], o["idle-session-timeout"], o["min-idle-session"] = 30, 30, 1
	}
	return o
}

func fallback(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

func (n Node) URI() string {
	if n.Protocol == "vmess" {
		v := Object{"v": "2", "ps": n.Name, "add": n.Server, "port": strconv.Itoa(n.Port), "id": n.UUID, "aid": "0", "scy": fallback(n.Cipher, "auto"), "net": fallback(n.Transport.Type, "tcp"), "type": "none", "host": n.Transport.Host, "path": n.Transport.Path, "tls": "", "sni": n.TLS.ServerName}
		if n.Transport.Type == "grpc" {
			v["path"] = n.Transport.ServiceName
		}
		if n.TLS.Enabled {
			v["tls"] = "tls"
			if n.TLS.Fingerprint != "" {
				v["fp"] = n.TLS.Fingerprint
			}
			if len(n.TLS.ALPN) > 0 {
				v["alpn"] = strings.Join(n.TLS.ALPN, ",")
			}
			if n.TLS.Insecure {
				v["insecure"] = "1"
			}
		}
		data, _ := json.Marshal(v)
		return "vmess://" + base64.StdEncoding.EncodeToString(data)
	}
	u := url.URL{Scheme: n.Protocol, Host: net.JoinHostPort(n.Server, strconv.Itoa(n.Port)), Fragment: n.Name}
	q := url.Values{}
	if n.TLS.Enabled {
		q.Set("sni", n.TLS.ServerName)
		if n.TLS.Fingerprint != "" {
			q.Set("fp", n.TLS.Fingerprint)
		}
		if n.TLS.Insecure {
			q.Set("insecure", "1")
		}
		if len(n.TLS.ALPN) > 0 {
			q.Set("alpn", strings.Join(n.TLS.ALPN, ","))
		}
	}
	switch n.Protocol {
	case "vless":
		u.User = url.User(n.UUID)
		q.Set("encryption", "none")
		q.Set("type", fallback(n.Transport.Type, "tcp"))
		if n.Flow != "" {
			q.Set("flow", n.Flow)
		}
		if n.TLS.Enabled {
			q.Set("security", "tls")
		} else {
			q.Set("security", "none")
		}
		if n.TLS.RealityKey != "" {
			q.Set("security", "reality")
			q.Set("pbk", n.TLS.RealityKey)
			q.Set("sid", n.TLS.RealityShortID)
			q.Set("fp", fallback(n.TLS.Fingerprint, "chrome"))
		}
	case "tuic":
		u.User = url.UserPassword(n.UUID, n.Password)
		q.Set("congestion_control", fallback(n.Congestion, "bbr"))
		q.Set("udp_relay_mode", fallback(n.UDPRelay, "native"))
		q.Set("allow_insecure", "0")
		if n.TLS.Insecure {
			q.Set("allow_insecure", "1")
		}
	case "ss":
		u.User = url.User(base64.RawURLEncoding.EncodeToString([]byte(n.Cipher + ":" + n.Password)))
	default:
		u.User = url.User(n.Password)
	}
	if n.Protocol == "hysteria2" {
		if n.TLS.CertificateHash != "" {
			q.Set("pinSHA256", n.TLS.CertificateHash)
		}
		if len(n.Ports) > 0 {
			q.Set("mport", strings.ReplaceAll(strings.Join(n.Ports, ","), ":", "-"))
		}
	}
	if n.Transport.Type == "ws" || n.Transport.Type == "httpupgrade" {
		q.Set("type", n.Transport.Type)
		q.Set("path", n.Transport.Path)
		q.Set("host", n.Transport.Host)
	}
	if n.Transport.Type == "grpc" {
		q.Set("type", "grpc")
		q.Set("serviceName", n.Transport.ServiceName)
	}
	if n.Obfs != "" {
		q.Set("obfs", n.Obfs)
		q.Set("obfs-password", n.ObfsPassword)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
