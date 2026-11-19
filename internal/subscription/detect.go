package subscription

import (
	"bytes"
	"encoding/json"
	"go.yaml.in/yaml/v3"
	"slices"
	"strings"
)

func detectFormat(data []byte) string {
	text := strings.TrimSpace(string(data))
	hasProxySection := strings.Contains(text, "[Proxy]")
	if strings.HasPrefix(text, "[server_local]") || strings.Contains(text, "\n[server_local]") {
		return "quantumult-x"
	}
	if hasProxySection {
		if strings.Contains(text, "transport=") || strings.Contains(text, "over-tls=") {
			return "loon"
		}
	}
	if bytes.HasPrefix(data, []byte("[")) && json.Valid(data) {
		return "nodes"
	}
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "//") {
			continue
		}
		if scheme, _, ok := strings.Cut(line, "://"); ok && !strings.ContainsAny(scheme, " =,\t") {
			return "uri"
		}
		if left, right, ok := strings.Cut(line, "="); ok && strings.Contains(right, ",") {
			fields, err := splitTextFields(right)
			if hasProxySection && (err != nil || len(fields) < 4) {
				continue
			}
			if slices.Contains([]string{"vmess", "vless", "trojan", "shadowsocks", "anytls"}, strings.TrimSpace(left)) && strings.Contains(right, "tag=") {
				return "quantumult-x"
			}
			if strings.Contains(line, "transport=") || strings.Contains(line, "over-tls=") || (err == nil && len(fields) > 3 && !strings.Contains(fields[3], "=")) {
				return "loon"
			}
			return "surge"
		}
		if !hasProxySection {
			break
		}
	}
	var tree yaml.Node
	if yaml.Unmarshal(data, &tree) == nil && validateYAMLTree(&tree, 0) == nil {
		var doc map[string]any
		if tree.Decode(&doc) == nil {
			if _, ok := doc["proxies"]; ok {
				return "mihomo"
			}
			if _, ok := doc["outbounds"]; ok {
				return "sing-box"
			}
		}
	}
	if decoded, err := decodeBase64(strings.Join(strings.Fields(text), "")); err == nil && bytes.Contains(decoded, []byte("://")) {
		return "base64"
	}
	return ""
}
