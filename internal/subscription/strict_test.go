package subscription

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestImportRejectsUnrepresentedOrMalformedFields(t *testing.T) {
	for _, tc := range []struct{ format, body string }{
		{"nodes", "[] []"},
		{"mihomo", "proxies: []\n---\nproxies: []"},
		{"mihomo", "proxies: [{name: test, type: trojan, server: localhost, port: 443.5, password: secret}]"},
		{"mihomo", "proxies: [{name: test, type: trojan, server: localhost, port: 443, password: secret, smux: {enabled: true}}]"},
		{"sing-box", `{"outbounds":[{"type":"trojan","tag":"test","server":"localhost","server_port":443,"password":"secret","tls":{"enabled":true,"min_version":"1.3"}}]}`},
		{"uri", "vless://00000000-0000-4000-8000-000000000001@localhost:443?security=unknown#test"},
		{"uri", "trojan://secret@localhost:443?insecure=maybe#test"},
		{"uri", "trojan://secret@localhost:443?sni=a&sni=b#test"},
	} {
		if _, err := Parse([]byte(tc.body), tc.format, "source"); err == nil {
			t.Fatalf("accepted unsupported %s input", tc.format)
		}
	}
	for _, node := range testNodes() {
		data, _ := json.Marshal(Object{"outbounds": []Object{node.SingBox()}})
		if _, err := Parse(data, "sing-box", "source"); err != nil {
			t.Fatalf("own native output cannot import %s: %v", node.Protocol, err)
		}
	}
	node := testNodes()[0]
	node.UUID = strings.Repeat("a", 36)
	if node.Validate() == nil {
		t.Fatal("nonstandard UUID accepted")
	}
}
