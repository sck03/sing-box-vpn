package subscription

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

func splitTextFields(line string) ([]string, error) {
	var fields []string
	quoted, start := false, 0
	for i, c := range line {
		if c == '"' {
			quoted = !quoted
		}
		if c == ',' && !quoted {
			fields = append(fields, strings.TrimSpace(line[start:i]))
			start = i + 1
		}
	}
	if quoted {
		return nil, errors.New("unbalanced field quotes")
	}
	return append(fields, strings.TrimSpace(line[start:])), nil
}

func parseTextNode(line, client string) (Node, error) {
	var n Node
	left, right, ok := strings.Cut(line, "=")
	if !ok {
		return n, errors.New("missing node declaration")
	}
	fields, err := splitTextFields(right)
	if err != nil {
		return n, err
	}
	offset := 0
	if client == "quantumult-x" {
		n.Protocol = strings.TrimSpace(left)
		if len(fields) < 1 {
			return n, errors.New("missing endpoint")
		}
		host, port, err := net.SplitHostPort(fields[0])
		if err != nil {
			return n, errors.New("invalid endpoint")
		}
		n.Server, n.Port, offset = host, integer(port), 1
	} else {
		if len(fields) < 3 {
			return n, errors.New("missing endpoint")
		}
		n.Name, n.Protocol, n.Server, n.Port, offset = strings.TrimSpace(left), strings.ToLower(fields[0]), fields[1], integer(fields[2]), 3
		if n.Protocol == "tuic-v5" {
			n.Protocol = "tuic"
		}
		if client == "loon" {
			if n.Protocol == "vmess" || n.Protocol == "shadowsocks" || n.Protocol == "ss" {
				if len(fields) <= offset {
					return n, errors.New("missing cipher")
				}
				n.Cipher = fields[offset]
				offset++
			}
			if len(fields) <= offset {
				return n, errors.New("missing credential")
			}
			credential := strings.Trim(fields[offset], "\"")
			offset++
			if n.Protocol == "vmess" || n.Protocol == "vless" {
				n.UUID = credential
			} else {
				n.Password = credential
			}
		}
	}
	n = normalizeNode(n)
	n.TLS.Enabled = n.Protocol == "trojan" || n.Protocol == "hysteria2" || n.Protocol == "tuic" || n.Protocol == "anytls"
	seen := map[string]bool{}
	for _, field := range fields[offset:] {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return n, errors.New("invalid connection field")
		}
		key, value = strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), "\"")
		if seen[key] {
			return n, errors.New("duplicate connection field")
		}
		seen[key] = true
		truth := func() (bool, error) { return strconv.ParseBool(value) }
		switch key {
		case "tag":
			n.Name = value
		case "username", "uuid":
			n.UUID = value
		case "password":
			if n.Protocol == "vmess" || n.Protocol == "vless" {
				n.UUID = value
			} else {
				n.Password = value
			}
		case "encrypt-method", "method":
			if n.Protocol == "ss" || n.Protocol == "vmess" {
				n.Cipher = value
			} else if value != "none" {
				return n, errors.New("unsupported encryption")
			}
		case "sni", "tls-name", "tls-host":
			n.TLS.ServerName = value
		case "tls", "over-tls":
			n.TLS.Enabled, err = truth()
		case "skip-cert-verify":
			n.TLS.Insecure, err = truth()
		case "tls-verification":
			var verify bool
			verify, err = truth()
			n.TLS.Insecure = !verify
		case "transport":
			n.Transport.Type = value
		case "ws":
			var enabled bool
			enabled, err = truth()
			if enabled {
				n.Transport.Type = "ws"
			}
		case "ws-path", "path", "obfs-uri":
			n.Transport.Path = value
		case "host", "obfs-host":
			n.Transport.Host = value
		case "ws-headers":
			if !strings.HasPrefix(value, "Host:") {
				return n, errors.New("unsupported WebSocket headers")
			}
			n.Transport.Host = strings.TrimSpace(strings.TrimPrefix(value, "Host:"))
		case "obfs":
			switch value {
			case "ws", "wss":
				n.Transport.Type = "ws"
				n.TLS.Enabled = value == "wss"
			case "over-tls":
				n.TLS.Enabled = true
			default:
				return n, errors.New("unsupported obfuscation")
			}
		case "flow", "vless-flow":
			n.Flow = value
		case "reality-public-key", "public-key", "reality-base64-pubkey":
			n.TLS.RealityKey = value
		case "reality-short-id", "short-id", "reality-hex-shortid":
			n.TLS.RealityShortID = value
		case "server-cert-fingerprint-sha256":
			n.TLS.CertificateHash = value
		case "alpn":
			n.TLS.ALPN = strings.Split(value, ",")
		case "tls-alpn":
			var data []byte
			data, err = hex.DecodeString(value)
			for err == nil && len(data) > 0 {
				size := int(data[0])
				data = data[1:]
				if size == 0 || size > len(data) {
					err = errors.New("invalid ALPN")
					break
				}
				n.TLS.ALPN = append(n.TLS.ALPN, string(data[:size]))
				data = data[size:]
			}
		case "port-hopping":
			n.Ports = strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == ',' })
		case "udp", "udp-relay", "vmess-aead", "aead":
			var enabled bool
			enabled, err = truth()
			if err == nil && !enabled {
				err = errors.New("disabled UDP or non-AEAD VMess is unsupported")
			}
		default:
			return n, fmt.Errorf("unsupported connection field %s", key)
		}
		if err != nil {
			return n, fmt.Errorf("invalid connection field %s", key)
		}
	}
	if n.Name == "" {
		n.Name = n.Server
	}
	n = normalizeNode(n)
	return n, n.Validate()
}
