package subscription

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Normalize only equivalent spellings; connection options are never discarded.
func normalizeNode(n Node) Node {
	n.Protocol = strings.ToLower(n.Protocol)
	switch n.Protocol {
	case "shadowsocks":
		n.Protocol = "ss"
	case "hy2":
		n.Protocol = "hysteria2"
	}
	n.Cipher = strings.ToLower(n.Cipher)
	if n.Protocol == "vmess" && n.Cipher == "chacha20-ietf-poly1305" {
		n.Cipher = "chacha20-poly1305"
	}
	if n.Protocol == "ss" && n.Cipher == "chacha20-poly1305" {
		n.Cipher = "chacha20-ietf-poly1305"
	}
	if n.Transport.Type == "websocket" {
		n.Transport.Type = "ws"
	}
	if (n.Transport.Type == "ws" || n.Transport.Type == "httpupgrade") && n.Transport.Path == "" {
		n.Transport.Path = "/"
	}
	return n
}

func uniqueNodeName(base string, used map[string]bool) string {
	name := base
	for i := 2; used[name] || reservedName(name); i++ {
		suffix := " · " + strconv.Itoa(i)
		prefix := base
		for len(prefix)+len(suffix) > 256 {
			_, size := utf8.DecodeLastRuneInString(prefix)
			prefix = prefix[:len(prefix)-size]
		}
		name = prefix + suffix
	}
	return name
}

func vmessMethod(cipher, client string) string {
	cipher = fallback(cipher, "auto")
	switch client {
	case "quantumult-x":
		if cipher == "auto" {
			return "chacha20-poly1305"
		}
	case "loon":
		if cipher == "chacha20-poly1305" {
			return "chacha20-ietf-poly1305"
		}
	}
	return cipher
}
