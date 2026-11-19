package manage

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"
)

func secretField(key string) bool {
	return slices.Contains([]string{"uuid", "password", "passwords", "private_key", "reality_private", "private", "token", "token_hash", "key", "secret", "authorization", "headers", "vpn_secrets", "proxy_pass", "socks_pass", "url", "webhook", "content", "client_secret", "pre_shared_key"}, key)
}

// Keep bounded, printable validation context without publishing values from
// secret fields. The raw configuration itself is never included in errors.
func validationDiagnostic(output, config []byte) string {
	text := string(output)
	var root any
	if json.Unmarshal(config, &root) != nil {
		return ""
	}
	var visit func(any, bool)
	visit = func(value any, secret bool) {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				visit(child, secret || secretField(key))
			}
		case []any:
			for _, child := range v {
				visit(child, secret)
			}
		case string:
			if secret && v != "" {
				text = strings.ReplaceAll(text, v, "[redacted]")
			}
		}
	}
	visit(root, false)
	text = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text))
	runes := []rune(text)
	if len(runes) > 4096 {
		text = string(runes[:4096]) + "…"
	}
	if text == "" {
		return ""
	}
	return ": " + text
}
