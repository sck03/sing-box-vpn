package subscription

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestAutoFormatsAndPartialSourceDiagnostics(t *testing.T) {
	n := testNodes()[1]
	n.TLS.Fingerprint = ""
	for _, format := range []string{"uri", "base64", "nodes", "sing-box", "mihomo", "surge", "loon", "quantumult-x"} {
		t.Run(format, func(t *testing.T) {
			var data []byte
			switch format {
			case "uri":
				data = []byte(n.URI())
			case "base64":
				data = []byte(base64.StdEncoding.EncodeToString([]byte(n.URI())))
			case "nodes":
				data, _ = json.Marshal([]Node{n})
			case "sing-box":
				data, _ = json.Marshal(Object{"outbounds": []Object{n.SingBox()}})
			case "mihomo":
				data, _ = json.Marshal(Object{"proxies": []Object{n.Mihomo()}})
			default:
				a, _, err := Render([]Node{n}, Template{ID: "test", Client: format, Version: 1, Mode: "nodes"}, false)
				if err != nil {
					t.Fatal(err)
				}
				data = a.Data
			}
			p, err := ParseSource(append([]byte{0xef, 0xbb, 0xbf}, data...), Source{ID: "source", Format: "auto"})
			if err != nil || len(p.Nodes) != 1 || p.Nodes[0].UUID != n.UUID || p.Nodes[0].Transport != n.Transport {
				t.Fatalf("%s: %+v, %v", format, p, err)
			}
		})
	}
	data := []byte(n.URI() + "\nunsupported://credential@localhost:443\n")
	if _, err := Parse(data, "uri", "source"); err == nil {
		t.Fatal("strict parse accepted invalid node")
	}
	p, err := ParseSource(data, Source{ID: "source", Format: "auto", SkipInvalid: true})
	if err != nil || len(p.Nodes) != 1 || len(p.Diagnostics) != 1 {
		t.Fatal(p, err)
	}
	if _, err := ParseSource([]byte("bad://x@host:443"), Source{Format: "uri", SkipInvalid: true}); err == nil {
		t.Fatal("all-invalid source accepted")
	}
	if p, err := ParseSource([]byte("vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&alpn=h2,http/1.1#test"), Source{Format: "auto"}); err != nil || p.Format != "uri" {
		t.Fatal("URI query mistaken for text", err)
	}
	if _, err := ParseSource([]byte(strings.Repeat(n.URI()+"\n", MaxNodes+1)), Source{Format: "uri", SkipInvalid: true}); err == nil {
		t.Fatal("node attempt budget bypassed")
	}
}

func TestExtendedConnectionRoundTripsAndLossRejection(t *testing.T) {
	for _, kind := range []string{"grpc", "httpupgrade", "salamander"} {
		n := testNodes()[1]
		n.TLS.Fingerprint = ""
		n.Transport = Transport{Type: kind, Path: "/upgrade", Host: "ws.example.com"}
		if kind == "grpc" {
			n.Transport = Transport{Type: kind, ServiceName: "service-name"}
		}
		if kind == "salamander" {
			n = testNodes()[2]
			n.Obfs, n.ObfsPassword = kind, "obfs-secret"
		}
		for _, format := range []string{"uri", "sing-box", "mihomo"} {
			var data []byte
			switch format {
			case "uri":
				data = []byte(n.URI())
			case "sing-box":
				data, _ = json.Marshal(Object{"outbounds": []Object{n.SingBox()}})
			default:
				data, _ = json.Marshal(Object{"proxies": []Object{n.Mihomo()}})
			}
			parsed, err := Parse(data, format, "source")
			if err != nil || len(parsed) != 1 {
				t.Fatal(kind, format, err)
			}
			if parsed[0].Transport != n.Transport || parsed[0].Obfs != n.Obfs || parsed[0].ObfsPassword != n.ObfsPassword {
				t.Fatal("lost connection parameters", kind, format)
			}
		}
		if _, _, err := Render([]Node{n}, Template{Client: "loon", Version: 1, Mode: "nodes"}, false); err == nil {
			t.Fatal("lossy target accepted", kind)
		}
	}
	n := testNodes()[1]
	n.Transport = Transport{Type: "grpc", Path: "/ignored"}
	if err := n.Validate(); err == nil {
		t.Fatal("ignored gRPC path accepted")
	}
	n.Transport, n.Cipher = Transport{}, "zero"
	if _, _, err := Render([]Node{n}, Template{Client: "quantumult-x", Version: 1, Mode: "nodes"}, false); err == nil {
		t.Fatal("VMess zero changed to a different cipher")
	}
}

func TestNamesAreStableUniqueAndUTF8Safe(t *testing.T) {
	n := testNodes()[1]
	n.Name = strings.Repeat("港", 85)
	other := n
	other.Port++
	p, err := Process([]Node{n, other}, Processor{})
	if err != nil || len(p.Nodes) != 2 || p.Nodes[0].Name == p.Nodes[1].Name || !utf8.ValidString(p.Nodes[1].Name) || len(p.Nodes[1].Name) > 256 {
		t.Fatal(p, err)
	}
	n.Name = "PROXY"
	p, err = Process([]Node{n}, Processor{})
	if err != nil || reservedName(p.Nodes[0].Name) {
		t.Fatal(p, err)
	}
}

func cachedFixture() Config {
	n := testNodes()[1]
	return Config{Version: SchemaVersion, Revision: 1,
		Sources:     []Source{{ID: "source", Kind: "inline", Format: "uri", Content: n.URI(), Required: true}},
		Collections: []Collection{{ID: "collection", Sources: []string{"source"}}},
		Templates:   []Template{{ID: "template", Client: "sing-box", Version: 1, Mode: "config"}},
		Outputs:     []Output{{ID: "output", Name: "First", Collection: "collection", Template: "template"}},
	}
}

func TestCompilerCacheInvalidationIntegrityAndDiagnostics(t *testing.T) {
	c := cachedFixture()
	c.Sources[0].IntervalSeconds = 1 << 60
	if err := c.Validate(); err == nil {
		t.Fatal("refresh duration overflow accepted")
	}
	c.Sources[0].IntervalSeconds = 0
	build := func(previous Snapshot) Snapshot {
		t.Helper()
		s, _, err := CompileCached(c, Runtime{}, nil, time.Now(), Strict, previous)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	first := build(Snapshot{})
	c.Outputs[0].Name = "Updated"
	second := build(first)
	if first.InputDigests["output"] != second.InputDigests["output"] || second.Artifacts["output"].Name != "Updated" || !bytes.Equal(first.Artifacts["output"].Data, second.Artifacts["output"].Data) {
		t.Fatal("unchanged compile failed")
	}
	c.Templates[0].TUN = true
	third := build(second)
	if third.InputDigests["output"] == second.InputDigests["output"] {
		t.Fatal("template change did not invalidate")
	}
	a := third.Artifacts["output"]
	a.Data = []byte("corrupted")
	third.Artifacts["output"] = a
	repaired := build(third)
	if !json.Valid(repaired.Artifacts["output"].Data) {
		t.Fatal("corrupted cache reused")
	}
	c.Sources[0].Content += "\nbad://x@host:443"
	c.Sources[0].SkipInvalid = true
	s := build(repaired)
	if s.NodeCounts["output"] != 1 || len(s.Diagnostics["output"]) == 0 || s.Diagnostics["output"][0].Stage != "parse" {
		t.Fatal("source diagnostic lost", s.Diagnostics)
	}
	c.Version--
	if _, _, err := Compile(c, Runtime{}, nil, time.Now(), Strict); err == nil {
		t.Fatal("old schema accepted")
	}
}

func TestTemplatePolicySafetyAndOrder(t *testing.T) {
	n := testNodes()[1]
	template := Template{ID: "template", Client: "mihomo", Version: 1, Mode: "config", Preset: "split",
		Groups:    []PolicyGroup{{ID: "Region", Type: "urltest", Scope: "normal"}},
		Providers: []RuleProvider{{ID: "Custom", URL: "https://example.com/rules.mrs", Type: "domain", Format: "mrs", Target: "Region"}},
		Rules:     []Rule{{Type: "domain", Value: "local.example.com", Target: "DIRECT"}},
		Native:    Object{"external-controller": "127.0.0.1:9090", "rules": []string{"MATCH,DIRECT"}},
	}
	a, _, err := Render([]Node{n}, template, false)
	if err != nil {
		t.Fatal(err)
	}
	var tree Object
	if err := json.Unmarshal(a.Data, &tree); err != nil {
		t.Fatal(err)
	}
	rules := stringList(tree["rules"])
	if len(rules) != 5 || rules[0] != "DOMAIN,local.example.com,DIRECT" || rules[1] != "RULE-SET,Custom,Region" || rules[4] != "MATCH,PROXY" {
		t.Fatal(rules)
	}
	if tree["external-controller"] != "127.0.0.1:9090" {
		t.Fatal("native setting lost")
	}
	template.Groups = []PolicyGroup{{ID: "a", Type: "select", Members: []string{"b"}}, {ID: "b", Type: "select", Members: []string{"a"}}}
	if err := template.Validate(); err == nil {
		t.Fatal("group cycle accepted")
	}
	template.Groups = []PolicyGroup{{ID: "Region", Type: "select", Include: "does-not-match"}}
	if _, _, err := Render([]Node{n}, template, false); err == nil {
		t.Fatal("empty group accepted")
	}
	n.Gateway = true
	template.Groups, template.Native, template.Preset = nil, nil, "proxy"
	template.Providers[0].Target = "DIRECT"
	if _, _, err := Render([]Node{n}, template, false); err == nil {
		t.Fatal("VPNGate bypass accepted")
	}
}

func TestExtendedTemplatesWithInstalledCores(t *testing.T) {
	for _, client := range []string{"sing-box", "mihomo"} {
		t.Run(client, func(t *testing.T) {
			env := "SING_BOX_BINARY"
			if client == "mihomo" {
				env = "MIHOMO_BINARY"
			}
			binary := os.Getenv(env)
			if binary == "" {
				t.Skip("core not configured")
			}
			n := testNodes()[1]
			n.Transport = Transport{Type: "grpc", ServiceName: "service"}
			upgrade := n
			upgrade.Name = "HTTPUpgrade"
			upgrade.Transport = Transport{Type: "httpupgrade", Host: "ws.example.com", Path: "/upgrade"}
			hy2 := testNodes()[2]
			hy2.Obfs, hy2.ObfsPassword = "salamander", "obfs-secret"
			policy := &DNSPolicy{Local: []string{"https://dns.alidns.com/dns-query"}, Remote: []string{"tls://one.one.one.one:853"}, Bootstrap: []string{"223.5.5.5"}}
			template := Template{ID: "test", Client: client, Version: 1, Mode: "config", Preset: "split", DNSPolicy: policy, Groups: []PolicyGroup{{ID: "Region", Type: "urltest", Scope: "normal", Tolerance: 50}}}
			if client == "mihomo" {
				policy.FakeIP, policy.IPv6 = true, true
			}
			a, _, err := Render([]Node{n, upgrade, hy2}, template, false)
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
				t.Fatal(err, string(out))
			}
		})
	}
}
