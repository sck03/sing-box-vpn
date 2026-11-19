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
	if n.Transport.Type == "grpc" && n.Transport.Mode == "gun" || n.Transport.Type == "xhttp" && n.Transport.Mode == "auto" {
		n.Transport.Mode = ""
	}
	if (n.Transport.Type == "ws" || n.Transport.Type == "httpupgrade" || n.Transport.Type == "xhttp") && n.Transport.Path == "" {
		n.Transport.Path = "/"
	}
	n.Transport = normalizeWebSocketPath(n.Transport)
	return n
}

type nodeNames struct {
	used map[string]bool
	next map[string]int
}

func newNodeNames() *nodeNames {
	return &nodeNames{used: map[string]bool{}, next: map[string]int{}}
}

func (n *nodeNames) unique(base string) string {
	name := base
	for i := max(2, n.next[base]); n.used[name] || reservedName(name); i++ {
		suffix := " · " + strconv.Itoa(i)
		prefix := base
		for len(prefix)+len(suffix) > 256 {
			_, size := utf8.DecodeLastRuneInString(prefix)
			prefix = prefix[:len(prefix)-size]
		}
		name = prefix + suffix
		n.next[base] = i + 1
	}
	n.used[name] = true
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
