package subscription

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPasswordURIAndDefaultPort(t *testing.T) {
	for _, auth := range []string{"alice:pass", "alice%3Apass", "alice:p%40ss"} {
		n, err := ParseURI("hysteria2://" + auth + "@example.com/?sni=example.com#sample")
		want := "alice:pass"
		if auth == "alice:p%40ss" {
			want = "alice:p@ss"
		}
		if err != nil || n.Port != 443 || n.Password != want {
			t.Fatalf("%q: %+v %v", auth, n, err)
		}
		back, err := ParseURI(n.URI())
		if err != nil || back.Password != want {
			t.Fatal("password roundtrip", err)
		}
	}
}

func portableNodes() []Node {
	base := Node{Name: "导入节点", Server: "127.0.0.1", Port: 14443, TLS: TLS{Enabled: true, ServerName: "example.com"}}
	var nodes []Node
	for _, transport := range []Transport{{Type: "ws", Path: "/ws", Host: "example.com"}, {Type: "grpc", ServiceName: "test"}, {Type: "httpupgrade", Path: "/upgrade", Host: "example.com"}} {
		n := base
		n.Protocol = "trojan"
		n.Password = "test-password"
		n.Transport = transport
		nodes = append(nodes, n)
	}
	n := base
	n.Protocol = "vless"
	n.UUID = "00000000-0000-4000-8000-000000000001"
	n.Transport = Transport{Type: "ws", Path: "/ws", Host: "example.com", EarlyData: 2048, EarlyDataHeader: "Sec-WebSocket-Protocol"}
	nodes = append(nodes, n)
	for _, p := range []Plugin{{Type: "obfs", Mode: "http", Host: "example.com"}, {Type: "v2ray-plugin", Mode: "websocket", Host: "example.com", Path: "/plugin", TLS: true, Mux: true}} {
		n := Node{Name: "插件", Protocol: "ss", Server: "127.0.0.1", Port: 14444, Cipher: "aes-128-gcm", Password: "plugin-password", Plugin: &p}
		nodes = append(nodes, n)
	}
	return nodes
}

func TestPortableConnectionOptionsRoundtrip(t *testing.T) {
	for _, n := range portableNodes() {
		for _, client := range []string{"sing-box", "mihomo", "uri"} {
			t.Run(n.Protocol+"/"+n.Transport.Type+"/"+client+fmt.Sprint(n.Plugin), func(t *testing.T) {
				a, _, err := Render([]Node{n}, Template{Client: client, Version: 1, Mode: "nodes"}, false)
				if err != nil {
					t.Fatal(err)
				}
				back, err := Parse(a.Data, client, "source")
				if err != nil {
					t.Fatal(err)
				}
				if len(back) != 1 || !reflect.DeepEqual(back[0].Transport, n.Transport) || !reflect.DeepEqual(back[0].Plugin, n.Plugin) {
					t.Fatalf("connection parameters changed: %+v", back)
				}
			})
		}
	}
}

func TestXHTTPAndGRPCClientBoundaries(t *testing.T) {
	n, err := ParseURI(`vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&type=xhttp&path=%2Fx&mode=packet-up&extra=%7B%22scMaxEachPostBytes%22%3A1000000%2C%22noSSEHeader%22%3Atrue%7D#XHTTP`)
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []string{"uri", "mihomo"} {
		a, _, err := Render([]Node{n}, Template{Client: client, Version: 1, Mode: "nodes"}, false)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Parse(a.Data, client, "source")
		if err != nil || !reflect.DeepEqual(back[0].Transport, n.Transport) {
			t.Fatalf("%s: %+v %v", client, back, err)
		}
	}
	if _, _, err := Render([]Node{n}, Template{Client: "sing-box", Version: 1, Mode: "nodes"}, false); err == nil {
		t.Fatal("sing-box accepted XHTTP")
	}
	n.Transport = Transport{Type: "grpc", Mode: "multi", Authority: "edge.example.com", ServiceName: "service"}
	for _, client := range []string{"surge", "uri"} {
		a, _, err := Render([]Node{n}, Template{Client: client, Version: 1, Mode: "nodes"}, false)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Parse(a.Data, client, "source")
		if err != nil || back[0].Transport != n.Transport {
			t.Fatal("gRPC parameters lost", client, err)
		}
	}
	if _, _, err := Render([]Node{n}, Template{Client: "mihomo", Version: 1, Mode: "nodes"}, false); err == nil {
		t.Fatal("ignored gRPC authority")
	}
}

func TestImportedNodesWithInstalledCores(t *testing.T) {
	for _, client := range []string{"sing-box", "mihomo"} {
		env := "SING_BOX_BINARY"
		if client == "mihomo" {
			env = "MIHOMO_BINARY"
		}
		binary := os.Getenv(env)
		if binary == "" {
			t.Skip("core not configured")
		}
		nodes := portableNodes()
		for i := range nodes {
			nodes[i].Name = fmt.Sprint("node-", i)
		}
		a, _, err := Render(nodes, Template{Client: client, Version: 1, Mode: "config"}, false)
		if err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		path := filepath.Join(root, "config.json")
		if err := os.WriteFile(path, a.Data, 0600); err != nil {
			t.Fatal(err)
		}
		args := []string{"check", "-c", path}
		if client == "mihomo" {
			args = []string{"-t", "-d", root, "-f", path}
		}
		if out, err := exec.Command(binary, args...).CombinedOutput(); err != nil {
			t.Fatal(client, err, string(out))
		}
	}
}

func TestNameAllocatorNaturalCollisions(t *testing.T) {
	n := newNodeNames()
	names := []string{"节点 · 2", "节点", "节点", "节点", "PROXY", "PROXY"}
	want := []string{"节点 · 2", "节点", "节点 · 3", "节点 · 4", "PROXY · 2", "PROXY · 3"}
	for i, name := range names {
		if got := n.unique(name); got != want[i] {
			t.Fatalf("%q != %q", got, want[i])
		}
	}
}

func BenchmarkRepeatedNodeNames(b *testing.B) {
	nodes := make([]Node, 4096)
	for i := range nodes {
		nodes[i] = Node{Name: strings.Repeat("港", 80), Protocol: "ss", Cipher: "aes-128-gcm", Password: "test", Server: "example.com", Port: i + 1}
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Process(nodes, Processor{}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestExplicitFakeIPDisable(t *testing.T) {
	n := portableNodes()[0]
	a, _, err := Render([]Node{n}, Template{Client: "mihomo", Version: 1, Mode: "config", DNSPolicy: &DNSPolicy{FakeIP: false}}, false)
	if err != nil {
		t.Fatal(err)
	}
	var config Object
	_ = json.Unmarshal(a.Data, &config)
	if obj(config["dns"])["enhanced-mode"] != "redir-host" {
		t.Fatal("explicit false was discarded")
	}
}

func TestNestedPolicyScopeAndPresetNames(t *testing.T) {
	n := testNodes()[1]
	n.Name = "普通"
	n.TLS.Fingerprint = ""
	g := n
	g.Name = "日本"
	g.Gateway = true
	template := Template{Client: "mihomo", Version: 1, Mode: "config", Groups: []PolicyGroup{
		{ID: "ordinary", Name: "普通选择", Type: "select", Scope: "normal"},
		{ID: "gateway", Name: "日本选择", Type: "select", Scope: "vpngate", Members: []string{"ordinary"}},
	}}
	if _, _, err := Render([]Node{n, g}, template, false); err == nil {
		t.Fatal("nested group escaped its VPNGate scope")
	}
	for _, preset := range Presets() {
		for client, template := range preset.Templates {
			if _, ok := renderers[client]; !ok {
				t.Fatal("missing renderer", client)
			}
			if _, _, err := Render([]Node{n}, template, false); err != nil {
				t.Fatal(preset.ID, client, err)
			}
		}
		if preset.ID == "regional" {
			artifact, _, err := Render([]Node{g}, preset.Templates["mihomo"], false)
			if err != nil {
				t.Fatal(err)
			}
			var value Object
			_ = json.Unmarshal(artifact.Data, &value)
			found := false
			for _, item := range value["proxy-groups"].([]any) {
				v := obj(item)
				if v["name"] == "VPNGate" {
					found = strings.Contains(fmt.Sprint(v["proxies"]), "日本")
				}
			}
			if !found {
				t.Fatal("regional group is not reachable from VPNGate selector")
			}
		}
	}
}
