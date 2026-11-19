package subscription

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func (n Node) URI() string {
	if n.Protocol == "vmess" {
		v := Object{"v": "2", "ps": n.Name, "add": n.Server, "port": strconv.Itoa(n.Port), "id": n.UUID, "aid": "0", "scy": fallback(n.Cipher, "auto"), "net": fallback(n.Transport.Type, "tcp"), "type": "none", "host": n.Transport.Host, "path": n.Transport.Path, "tls": "", "sni": n.TLS.ServerName}
		if n.Transport.Type == "grpc" {
			v["path"] = n.Transport.ServiceName
		}
		if n.Transport.EarlyData > 0 {
			v["ed"], v["eh"] = n.Transport.EarlyData, n.Transport.EarlyDataHeader
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
		if n.Plugin != nil {
			q.Set("plugin", n.Plugin.Type+";"+n.Plugin.options())
		}
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
	if n.Transport.Type == "ws" || n.Transport.Type == "httpupgrade" || n.Transport.Type == "xhttp" {
		q.Set("type", n.Transport.Type)
		q.Set("path", n.Transport.Path)
		q.Set("host", n.Transport.Host)
	}
	if n.Transport.Type == "grpc" {
		q.Set("type", "grpc")
		q.Set("serviceName", n.Transport.ServiceName)
	}
	if n.Transport.Mode != "" {
		q.Set("mode", n.Transport.Mode)
	}
	if n.Transport.Authority != "" {
		q.Set("authority", n.Transport.Authority)
	}
	if n.Transport.EarlyData > 0 {
		q.Set("ed", strconv.Itoa(n.Transport.EarlyData))
		q.Set("eh", n.Transport.EarlyDataHeader)
	}
	if n.Transport.XHTTP != nil {
		data, _ := json.Marshal(n.Transport.XHTTP)
		q.Set("extra", string(data))
	}
	if n.Obfs != "" {
		q.Set("obfs", n.Obfs)
		q.Set("obfs-password", n.ObfsPassword)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
