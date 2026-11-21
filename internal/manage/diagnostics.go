package manage

import (
	"encoding/json"
	"strings"
	"unicode"
)

func secretField(key string) bool {
	// Configuration and task projections share this policy. URL families are
	// credentials too: provenance/probe addresses can carry signed query strings.
	key = strings.ToLower(key)
	switch key {
	case "uuid", "password", "passwords", "private", "token", "token_hash", "key", "secret", "secrets", "authorization", "headers", "url", "webhook", "content", "pre_shared_key":
		return true
	}
	for _, suffix := range []string{"_url", "_password", "_passwords", "_pass", "_token", "_token_hash", "_secret", "_secrets", "_private", "_private_key", "_key_pem"} {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return key == "private_key"
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
