package serverconfig

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func (n Node) Validate() error {
	invalid := func(field, message string) error { return &FieldError{Node: n.ID, Field: field, Message: message} }
	if !domain.ResourceID.MatchString(n.ID) {
		return invalid("id", "invalid resource ID")
	}
	if n.Port < 1 || n.Port > 65535 {
		return invalid("port", "must be within 1–65535")
	}
	switch n.Protocol {
	case "vless", "vmess", "tuic", "hysteria2", "anytls":
	default:
		return invalid("protocol", "unsupported managed protocol")
	}
	if n.Protocol != "vmess" && !n.TLS.Enabled {
		return invalid("tls.enabled", "TLS is required")
	}
	if n.Protocol == "vless" {
		if n.Flow != "" && n.Flow != "xtls-rprx-vision" {
			return invalid("flow", "unsupported flow")
		}
		key, err := base64.RawURLEncoding.DecodeString(n.TLS.RealityPrivate)
		if err != nil {
			return invalid("tls.reality_private", "invalid X25519 key")
		}
		if _, err = ecdh.X25519().NewPrivateKey(key); err != nil {
			return invalid("tls.reality_private", "invalid X25519 key")
		}
		if _, err = hex.DecodeString(n.TLS.RealityShortID); err != nil || len(n.TLS.RealityShortID) > 16 {
			return invalid("tls.reality_short_id", "expected up to 16 hexadecimal characters in byte pairs")
		}
		if n.TLS.HandshakeHost == "" || strings.ContainsAny(n.TLS.HandshakeHost, " /\r\n\x00") || n.TLS.HandshakePort < 1 || n.TLS.HandshakePort > 65535 {
			return invalid("tls.handshake_host", "valid Reality handshake host and port are required")
		}
		if n.TLS.Certificate != "" || n.TLS.Key != "" {
			return invalid("tls.certificate", "Reality does not use certificate files")
		}
	} else {
		if n.Flow != "" {
			return invalid("flow", "only supported by VLESS")
		}
		if n.TLS.RealityPrivate != "" || n.TLS.RealityShortID != "" || n.TLS.HandshakeHost != "" || n.TLS.HandshakePort != 0 {
			return invalid("tls", "Reality fields require VLESS")
		}
		if n.TLS.Enabled && (n.TLS.Certificate == "" || n.TLS.Key == "") {
			return invalid("tls.certificate", "certificate and key are required")
		}
	}
	if n.Protocol != "vmess" {
		if n.Transport != (Transport{}) {
			return invalid("transport", "only VMess supports a managed transport")
		}
		return nil
	}
	t := n.Transport
	if t.Type != "ws" {
		return invalid("transport.type", "managed VMess supports ws only")
	}
	if err := ValidateWebSocketPath(t.Path); err != nil {
		return invalid("transport.path", err.Error())
	}
	if strings.ContainsAny(t.Host, "\r\n\x00") {
		return invalid("transport.host", "invalid HTTP Host")
	}
	if t.EarlyData > 0 && t.EarlyDataHeader == "" && strings.ContainsFunc(t.Path, func(r rune) bool { return r > 127 }) {
		return invalid("transport.early_data_header", "a header is required for early data with a Unicode path")
	}
	if t.EarlyData < 0 || t.EarlyData > 65535 {
		return invalid("transport.early_data", "must be within 0–65535")
	}
	for _, c := range t.EarlyDataHeader {
		if c <= 32 || c >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}", c) {
			return invalid("transport.early_data_header", "invalid HTTP header name")
		}
	}
	return nil
}

func ValidateWebSocketPath(path string) error {
	if len(path) == 0 || len(path) > 255 || path[0] != '/' || !utf8.ValidString(path) ||
		strings.ContainsAny(path, "?#%\\") || strings.ContainsFunc(path, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) {
		return errors.New("WebSocket 路径须以 / 开头，最多 255 字节，不能包含查询参数、片段、百分号转义、反斜杠、空白或控制字符")
	}
	return nil
}
