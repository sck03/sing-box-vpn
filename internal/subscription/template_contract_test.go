package subscription

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/clientprofile"
)

func TestDNSInheritanceAcrossJSONRoundTrip(t *testing.T) {
	for _, client := range []string{"mihomo", "openclash", "stash"} {
		for _, tc := range []struct {
			policy, mode string
			filters      int
		}{
			{`{"remote":["1.1.1.1"]}`, "fake-ip", 3},
			{`{"fake_ip":true}`, "fake-ip", 3},
			{`{"fake_ip":false}`, "redir-host", 0},
			{`{"fake_ip_filter":[]}`, "fake-ip", 0},
		} {
			t.Run(client+tc.policy, func(t *testing.T) {
				template := Template{Client: client, Version: 1, Mode: "config"}
				if err := json.Unmarshal([]byte(tc.policy), &template.DNSPolicy); err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(template)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(data, &template); err != nil {
					t.Fatal(err)
				}
				a, _, err := Render([]Node{portableNodes()[0]}, template, false)
				if err != nil {
					t.Fatal(err)
				}
				var tree Object
				if err := json.Unmarshal(a.Data, &tree); err != nil {
					t.Fatal(err)
				}
				dns := obj(tree["dns"])
				if dns["enhanced-mode"] != tc.mode || len(stringList(dns["fake-ip-filter"])) != tc.filters {
					t.Fatal(dns)
				}
			})
		}
	}
}

func TestNativeFieldOwnership(t *testing.T) {
	for _, client := range []string{"sing-box", "mihomo", "openclash", "stash", "egern"} {
		fields := clientprofile.TemplateFields(client)
		for _, key := range append(fields.Managed, fields.Forbidden...) {
			template := Template{Client: client, Version: 1, Mode: "config", Native: Object{key: nil}}
			if err := template.Validate(); err == nil || !strings.Contains(err.Error(), "native."+key) {
				t.Fatalf("%s accepted %s: %v", client, key, err)
			}
		}
	}
	for _, client := range []string{"mihomo", "openclash", "stash"} {
		template := Template{Client: client, Version: 1, Mode: "config", Native: Object{"mixed-port": 17890, "log-level": "info"}}
		a, _, err := Render([]Node{portableNodes()[0]}, template, false)
		if err != nil {
			t.Fatal(err)
		}
		var tree Object
		if err := json.Unmarshal(a.Data, &tree); err != nil {
			t.Fatal(err)
		}
		if integer(tree["mixed-port"]) != 17890 || tree["log-level"] != "info" {
			t.Fatal("native defaults silently overwritten", tree)
		}
	}
}

func TestEgernRulesAndConnectionFields(t *testing.T) {
	nodes := []Node{
		{Name: "hy", Protocol: "hysteria2", Server: "example.com", Port: 443, Password: "secret", TLS: TLS{Enabled: true}, Ports: []string{"1000:2000"}, HopIntervalSeconds: 30, Obfs: "salamander", ObfsPassword: "obfs-secret"},
		{Name: "grpc", Protocol: "vless", Server: "example.com", Port: 443, UUID: "00000000-0000-4000-8000-000000000001", TLS: TLS{Enabled: true, CertificateHash: strings.Repeat("ab", 32)}, Transport: Transport{Type: "grpc", ServiceName: "service"}},
	}
	template := Template{Client: "egern", Version: 1, Mode: "config", PrivateNetwork: true, Rules: []Rule{{Type: "ip_cidr", Value: "2001:db8::/32", Target: "DIRECT"}}}
	a, _, err := Render(nodes, template, false)
	if err != nil {
		t.Fatal(err)
	}
	var tree Object
	if err := json.Unmarshal(a.Data, &tree); err != nil {
		t.Fatal(err)
	}
	rules := tree["rules"].([]any)
	seen := 0
	for _, value := range rules {
		rule := obj(value)
		if v := obj(rule["ip_cidr"]); strings.Contains(str(v["match"]), ":") {
			t.Fatal("IPv6 emitted as IPv4", rule)
		}
		if obj(rule["ip_cidr6"]) != nil {
			seen++
		}
	}
	if seen != 4 {
		t.Fatal("IPv6 private/custom rules missing", seen)
	}
	proxies := tree["proxies"].([]any)
	hy := obj(obj(proxies[0])["hysteria2"])
	if hy["udp_relay"] != true || hy["port_hopping"] != "1000-2000" || integer(hy["port_hopping_interval"]) != 30 || hy["obfs_password"] != "obfs-secret" {
		t.Fatal(hy)
	}
	grpc := obj(obj(obj(obj(proxies[1])["vless"])["transport"])["grpc"])
	if grpc["service_name"] != "service" || grpc["fingerprint_sha256"] != nodes[1].TLS.CertificateHash {
		t.Fatal(grpc)
	}
	nodes[1].TLS.ALPN = []string{"h2"}
	if _, _, err := Render(nodes[1:], template, false); err == nil {
		t.Fatal("unrepresented ALPN accepted")
	}
}

func TestHoppingIntervalRoundTripAndRejection(t *testing.T) {
	n := Node{Name: "hy", Protocol: "hysteria2", Server: "example.com", Port: 443, Password: "secret", TLS: TLS{Enabled: true}, Ports: []string{"1000:2000"}, HopIntervalSeconds: 30}
	for _, client := range []string{"sing-box", "mihomo"} {
		a, _, err := Render([]Node{n}, Template{Client: client, Version: 1, Mode: "nodes"}, false)
		if err != nil {
			t.Fatal(err)
		}
		result, err := Parse(a.Data, client, "test")
		if err != nil || len(result) != 1 || result[0].HopIntervalSeconds != 30 {
			t.Fatal(client, result, err)
		}
	}
	for _, client := range []string{"uri", "base64", "surge", "stash"} {
		profile, _ := clientprofile.Lookup(client)
		if err := capabilityError(n, client, profile); err == nil {
			t.Fatal("interval silently dropped by", client)
		}
	}
}
