package manage

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func vmessURIFixture() (Object, Profile) {
	return Object{
		"type": "vmess", "tag": "香港 <节点> 🚀", "server": "cdn.example.com", "server_port": 443,
		"uuid":      "00000000-0000-4000-8000-000000000001",
		"tls":       Object{"enabled": true, "server_name": "tls.example.com"},
		"transport": Object{"path": "/ws?name=中文&x=1", "headers": Object{"Host": "ws.example.com"}},
	}, Profile{TLSSNI: "fallback.example.com"}
}

func TestVMessURIKeepsUnicodeAndWebSocketHost(t *testing.T) {
	node, profile := vmessURIFixture()
	link, err := URI(node, profile)
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(link, "vmess://"))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"ps": text(node["tag"]), "host": "ws.example.com", "sni": "tls.example.com",
		"path": "/ws?name=中文&x=1", "tls": "tls", "add": "cdn.example.com", "port": "443",
	} {
		if value[key] != want {
			t.Errorf("%s = %q, want %q", key, value[key], want)
		}
	}
}

func BenchmarkVMessURI(b *testing.B) {
	node, profile := vmessURIFixture()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := URI(node, profile); err != nil {
			b.Fatal(err)
		}
	}
}

func TestVMessMihomoKeepsWebSocketHost(t *testing.T) {
	node, profile := vmessURIFixture()
	for _, host := range []string{"ws.example.com", ""} {
		object(object(node["transport"])["headers"])["Host"] = host
		got := MihomoNode(node, profile)
		want := host
		if want == "" {
			want = profile.TLSSNI
		}
		ws := object(got["ws-opts"])
		if object(ws["headers"])["Host"] != want || ws["path"] != object(node["transport"])["path"] {
			t.Errorf("WebSocket transport changed: %#v", ws)
		}
		if got["server"] != "cdn.example.com" || got["servername"] != "tls.example.com" {
			t.Errorf("server or SNI changed: %#v", got)
		}
	}
}
