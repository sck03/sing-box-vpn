package manage

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type queryPair struct{ key, value string }

func query(values []queryPair) string {
	var result strings.Builder
	// Most keys and values are already URL-safe; reserve their unescaped size.
	size := 0
	for _, value := range values {
		size += len(value.key) + len(value.value) + 2
	}
	result.Grow(size)
	for i, value := range values {
		if i > 0 {
			result.WriteByte('&')
		}
		result.WriteString(url.QueryEscape(value.key))
		result.WriteByte('=')
		result.WriteString(url.QueryEscape(value.value))
	}
	return result.String()
}
func quote(value string) string { return strings.ReplaceAll(url.QueryEscape(value), "+", "%20") }

func URI(n Object, p Profile) (string, error) { return uriWithCache(n, p, certificateCache{}) }
func uriWithCache(n Object, p Profile, cache certificateCache) (string, error) {
	kind := text(n["type"])
	tls := object(n["tls"])
	sni := text(tls["server_name"])
	if sni == "" {
		sni = p.TLSSNI
	}
	if kind == "vmess" {
		return vmessURI(n, p, tls, sni)
	}
	endpoint := net.JoinHostPort(text(n["server"]), strconv.Itoa(number(n["server_port"])))
	name := quote(text(n["tag"]))
	values := []queryPair{{"sni", sni}}
	insecure := "0"
	if p.SelfSigned {
		insecure = "1"
	}
	switch kind {
	case "vless":
		r := object(tls["reality"])
		values = append(values, queryPair{"encryption", "none"}, queryPair{"flow", "xtls-rprx-vision"}, queryPair{"security", "reality"}, queryPair{"fp", "chrome"}, queryPair{"pbk", text(r["public_key"])}, queryPair{"sid", text(r["short_id"])}, queryPair{"type", "tcp"})
		return "vless://" + text(n["uuid"]) + "@" + endpoint + "?" + query(values) + "#" + name, nil
	case "hysteria2":
		values = append(values, queryPair{"alpn", "h3"}, queryPair{"insecure", insecure})
		if p.SelfSigned {
			info, err := cache.get(p.Certificate)
			if err != nil {
				return "", err
			}
			values = append(values, queryPair{"pinSHA256", info.Hash})
		}
		if len(p.Hopping) > 0 {
			values = append(values, queryPair{"mport", strings.ReplaceAll(strings.Join(p.Hopping, ","), ":", "-")})
		}
		return "hysteria2://" + quote(text(n["password"])) + "@" + endpoint + "?" + query(values) + "#" + name, nil
	case "tuic":
		values = append(values, queryPair{"congestion_control", "bbr"}, queryPair{"udp_relay_mode", "native"}, queryPair{"alpn", "h3"}, queryPair{"allow_insecure", insecure})
		return "tuic://" + text(n["uuid"]) + ":" + quote(text(n["password"])) + "@" + endpoint + "?" + query(values) + "#" + name, nil
	case "anytls":
		values = append(values, queryPair{"insecure", insecure})
		return "anytls://" + quote(text(n["password"])) + "@" + endpoint + "?" + query(values) + "#" + name, nil
	}
	return "", errors.New("未知协议")
}

// VMess uses Base64 JSON, so it needs neither a URI authority nor query encoding.
func vmessURI(n Object, p Profile, tls Object, sni string) (string, error) {
	transport := object(n["transport"])
	value := struct {
		V        string `json:"v"`
		PS       string `json:"ps"`
		Add      string `json:"add"`
		Port     string `json:"port"`
		ID       string `json:"id"`
		AID      string `json:"aid"`
		SCY      string `json:"scy"`
		Net      string `json:"net"`
		Type     string `json:"type"`
		Host     string `json:"host"`
		Path     string `json:"path"`
		TLS      string `json:"tls"`
		SNI      string `json:"sni"`
		Insecure string `json:"insecure,omitempty"`
	}{V: "2", PS: text(n["tag"]), Add: text(n["server"]), Port: strconv.Itoa(number(n["server_port"])), ID: text(n["uuid"]), AID: "0", SCY: "auto", Net: "ws", Type: "none", Host: webSocketHost(transport, p.TLSSNI), Path: text(transport["path"]), SNI: sni}
	if tls["enabled"] == true {
		value.TLS = "tls"
		if p.SelfSigned {
			value.Insecure = "1"
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(data), nil
}
