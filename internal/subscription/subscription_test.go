package subscription

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func testNodes() []Node {
	var nodes []Node
	for i, protocol := range protocols {
		n := Node{ID: protocol, Name: protocol + "-node", Protocol: protocol, Server: "127.0.0.1", Port: 20000 + i, UUID: "00000000-0000-4000-8000-000000000001", Password: "secret-password", TLS: TLS{Enabled: protocol != "ss", ServerName: "example.com"}}
		if protocol == "ss" {
			n.Cipher = "aes-128-gcm"
		}
		if protocol == "vmess" {
			n.Cipher = "auto"
			n.Transport = Transport{Type: "ws", Path: "/ws", Host: "ws.example.com"}
		}
		if protocol == "vless" {
			n.Flow = "xtls-rprx-vision"
			n.TLS.RealityKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
			n.TLS.RealityShortID = "abcdef12"
			n.TLS.Fingerprint = "chrome"
		}
		if !slices.Contains([]string{"vmess", "vless", "tuic"}, protocol) {
			n.UUID = ""
		}
		if slices.Contains([]string{"vmess", "vless"}, protocol) {
			n.Password = ""
		}
		nodes = append(nodes, n)
	}
	return nodes
}

func TestURIUnicodeCredentialsTLSAndHopping(t *testing.T) {
	for _, n := range testNodes() {
		n.Name = "香港 <节点> 🚀"
		if n.Password != "" {
			n.Password = "空 格@:/?#%"
		}
		if n.Protocol == "hysteria2" {
			n.Ports = []string{"31000:31100", "31200"}
			n.TLS.CertificateHash = strings.Repeat("a", 64)
			n.TLS.Insecure = true
		}
		if n.Protocol == "vmess" {
			n.Transport.Path = "/ws?name=中文&x=1"
		}
		parsed, err := ParseURI(n.URI())
		if err != nil {
			t.Fatal(n.Protocol, err)
		}
		if parsed.Name != n.Name || parsed.Password != n.Password || parsed.Server != n.Server || parsed.UUID != n.UUID || (n.TLS.Enabled && parsed.TLS.ServerName != n.TLS.ServerName) {
			t.Fatalf("%s lost connection parameters", n.Protocol)
		}
		if n.Protocol == "hysteria2" {
			u, _ := url.Parse(n.URI())
			if u.Query().Get("mport") != "31000-31100,31200" || u.Query().Get("pinSHA256") != n.TLS.CertificateHash {
				t.Fatal("lost pin or hopping")
			}
		}
		if n.Protocol == "vmess" {
			if parsed.Transport != n.Transport {
				t.Fatal("lost independent WebSocket host/path")
			}
			m := n.Mihomo()
			if obj(obj(m["ws-opts"])["headers"])["Host"] != n.Transport.Host {
				t.Fatal("lost Mihomo host")
			}
		}
	}
}

func TestVMessTLSOptIn(t *testing.T) {
	n := testNodes()[1]
	for _, enabled := range []bool{false, true} {
		for _, insecure := range []bool{false, true} {
			n.TLS.Enabled, n.TLS.Insecure = enabled, insecure
			data, err := decodeBase64(strings.TrimPrefix(n.URI(), "vmess://"))
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			json.Unmarshal(data, &m)
			if (m["insecure"] == "1") != (enabled && insecure) {
				t.Fatal(m)
			}
		}
	}
}

func TestAllClientModesAndExplicitCapabilityFailures(t *testing.T) {
	for client, cap := range Capabilities() {
		for _, mode := range cap.Modes {
			t.Run(client+"/"+mode, func(t *testing.T) {
				var nodes []Node
				for _, n := range testNodes() {
					if slices.Contains(cap.Protocols, n.Protocol) {
						if !cap.Features.UTLS {
							n.TLS.Fingerprint = ""
						}
						nodes = append(nodes, n)
					}
				}
				a, _, err := Render(nodes, Template{ID: "test", Client: client, Version: 1, Mode: mode, Preset: "proxy"}, false)
				if err != nil {
					t.Fatal(err)
				}
				if len(a.Data) == 0 || a.ETag == "" {
					t.Fatal("empty artifact")
				}
				if strings.HasPrefix(a.MIME, "application/") {
					var value any
					if err := json.Unmarshal(a.Data, &value); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
	_, diagnostics, err := Render(testNodes(), Template{ID: "qx", Client: "quantumult-x", Version: 1, Mode: "nodes"}, false)
	if err == nil || len(diagnostics) == 0 {
		t.Fatal("unsupported protocol silently lost")
	}
	a, diagnostics, err := Render(testNodes(), Template{ID: "qx", Client: "quantumult-x", Version: 1, Mode: "nodes"}, true)
	if err != nil || len(diagnostics) != 3 || len(a.Data) == 0 {
		t.Fatal("explicit exclusion failed", err, diagnostics)
	}
}

func TestNativeTemplatesValidateWithInstalledCores(t *testing.T) {
	for _, client := range []string{"sing-box", "mihomo"} {
		t.Run(client, func(t *testing.T) {
			env := "SING_BOX_BINARY"
			if client == "mihomo" {
				env = "MIHOMO_BINARY"
			}
			binary := os.Getenv(env)
			if binary == "" {
				if os.Getenv("SBVPN_REQUIRE_CLIENT_CHECK") == "1" {
					t.Fatal("required acceptance core is not configured")
				}
				t.Skip("core not configured")
			}
			for _, gateway := range []bool{false, true} {
				nodes := testNodes()
				for i := range nodes {
					nodes[i].Gateway = gateway
				}
				a, _, err := Render(nodes, Template{ID: "test", Client: client, Version: 1, Mode: "config", Preset: "proxy", TUN: client == "sing-box"}, false)
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
				if output, err := exec.Command(binary, args...).CombinedOutput(); err != nil {
					t.Fatal(err, string(output))
				}
			}
		})
	}
}

func TestProcessingUsesConnectionIdentityAndPreservesGatewayIsolation(t *testing.T) {
	nodes := testNodes()[:1]
	duplicate := nodes[0]
	duplicate.ID = "duplicate"
	duplicate.Name = "renamed"
	nodes = append(nodes, duplicate)
	p, err := Process(nodes, Processor{Deduplicate: true, Prefix: "phone-"})
	if err != nil || p.Output != 1 || len(p.Diagnostics) != 2 {
		t.Fatal(p, err)
	}
	nodes[1].UUID = "00000000-0000-4000-8000-000000000002"
	p, err = Process(nodes, Processor{Deduplicate: true})
	if err != nil || p.Output != 2 {
		t.Fatal("credentials ignored during deduplication", err)
	}
	nodes[0].Gateway = true
	_, _, err = Render(nodes[:1], Template{ID: "vpn", Client: "sing-box", Version: 1, Mode: "config", Preset: "split"}, false)
	if err == nil {
		t.Fatal("VPNGate direct fallback accepted")
	}
	_, _, err = Render(nodes[:1], Template{ID: "vpn", Client: "mihomo", Version: 1, Mode: "config", Rules: []Rule{{Type: "domain", Value: "example.com", Target: "DIRECT"}}}, false)
	if err == nil {
		t.Fatal("VPNGate DIRECT rule accepted")
	}
}

func TestParsingRejectsLossyAndMaliciousInputs(t *testing.T) {
	for _, tc := range []struct{ format, data string }{{"mihomo", "proxies: &x [*x]"}, {"mihomo", "proxies:\n- type: vmess\n  name: test\n  server: localhost\n  port: 443\n  plugin: unsupported"}, {"uri", "vless://uuid@host:443?plugin=unknown"}, {"nodes", "null"}, {"nodes", "[]"}} {
		if _, err := Parse([]byte(tc.data), tc.format, "source"); err == nil {
			t.Fatal("invalid source accepted", tc)
		}
	}
	for _, format := range []string{"mihomo", "sing-box"} {
		n := testNodes()[1]
		var value Object
		if format == "mihomo" {
			value = Object{"proxies": []Object{n.Mihomo()}}
		} else {
			value = Object{"outbounds": []Object{n.SingBox()}}
		}
		data, _ := json.Marshal(value)
		nodes, err := Parse(data, format, "source")
		if err != nil || len(nodes) != 1 || nodes[0].Transport.Host != n.Transport.Host {
			t.Fatal(format, nodes, err)
		}
	}
}

func TestBuildRejectsStaleAndRemovedSourceCache(t *testing.T) {
	c := fixtureConfig()
	c.Sources[0] = Source{ID: "normal", Kind: "remote", URL: "https://example.com/sub", Format: "uri", Required: true, MaxStaleSeconds: 60}
	now := time.Now()
	r := Runtime{Sources: map[string]SourceCache{"normal": {SourceHash: SourceHash(c.Sources[0]), Nodes: testNodes(), Success: now}}}
	if _, _, err := Build(c, r, nil, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Build(c, r, nil, now.Add(time.Minute+time.Second)); err == nil {
		t.Fatal("stale required source accepted")
	}
	c.Sources[0].URL = "https://new.example/sub"
	if _, _, err := Build(c, r, nil, now); err == nil {
		t.Fatal("cache from removed source resurrected")
	}
}

var benchmarkArtifact Artifact

func BenchmarkClientRender(b *testing.B) {
	nodes := testNodes()
	for _, client := range []string{"sing-box", "mihomo", "base64"} {
		b.Run(client, func(b *testing.B) {
			mode := "config"
			if client == "base64" {
				mode = "nodes"
			}
			t := Template{ID: "bench", Client: client, Version: 1, Mode: mode}
			b.ReportAllocs()
			for b.Loop() {
				var err error
				benchmarkArtifact, _, err = Render(nodes, t, false)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
func BenchmarkVMessURI(b *testing.B) {
	n := testNodes()[1]
	b.ReportAllocs()
	for b.Loop() {
		_ = n.URI()
	}
}
