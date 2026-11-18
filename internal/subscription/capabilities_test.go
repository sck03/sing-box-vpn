package subscription

import (
	"strings"
	"testing"
	"time"
)

func TestUnavailableRequiredAccountIsIsolated(t *testing.T) {
	c := Config{Version: SchemaVersion,
		Sources:     []Source{{ID: "expired", Kind: "account", AccountID: "a", Required: true}, {ID: "healthy", Kind: "account", AccountID: "b", Scope: "normal", Required: true}},
		Collections: []Collection{{ID: "expired", Sources: []string{"expired"}}, {ID: "healthy", Sources: []string{"healthy"}}},
		Templates:   []Template{{ID: "native", Client: "sing-box", Version: 1, Mode: "nodes"}},
		Outputs:     []Output{{ID: "expired", Collection: "expired", Template: "native"}, {ID: "healthy", Collection: "healthy", Template: "native"}},
	}
	local := map[string][]Node{"account:healthy": testNodes()}
	if _, _, err := Build(c, Runtime{}, local, time.Now()); err == nil {
		t.Fatal("strict build accepted a missing required source")
	}
	snapshot, _, err := BuildAvailable(c, Runtime{}, local, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Artifacts) != 1 || len(snapshot.Artifacts["healthy"].Data) == 0 || len(snapshot.Diagnostics["expired"]) == 0 {
		t.Fatal("failure was not isolated", snapshot)
	}
}

func TestStashRejectsCertificatePinLoss(t *testing.T) {
	for _, n := range testNodes() {
		if n.Protocol != "hysteria2" {
			continue
		}
		n.TLS.Insecure = true
		n.TLS.CertificateHash = strings.Repeat("a", 64)
		for _, mode := range []string{"provider", "config"} {
			if _, _, err := Render([]Node{n}, Template{ID: "stash", Client: "stash", Version: 1, Mode: mode}, false); err == nil {
				t.Fatal("dropped a certificate pin", mode)
			}
		}
	}
}

func TestReservedNamesRejectedAtRenderingBoundary(t *testing.T) {
	for _, name := range []string{"AUTO", "auto", "Auto", "PROXY", "VPNGate"} {
		n := testNodes()[0]
		n.Name = name
		if _, _, err := Render([]Node{n}, Template{ID: "test", Client: "mihomo", Version: 1, Mode: "config", ProbeURL: "https://example.com"}, false); err == nil {
			t.Fatal("reserved node accepted", name)
		}
	}
}

func TestExplicitConnectionOptionsArePreservedOrRejected(t *testing.T) {
	for _, n := range testNodes() {
		if n.Protocol == "vless" {
			n.TLS.RealityKey, n.TLS.RealityShortID = "", ""
			n.TLS.Fingerprint = "firefox"
			parsed, err := ParseURI(n.URI())
			if err != nil || parsed.TLS.Fingerprint != "firefox" {
				t.Fatal("ordinary TLS URI lost fingerprint", err)
			}
			if _, _, err := Render([]Node{n}, Template{ID: "test", Client: "stash", Version: 1, Mode: "provider"}, false); err == nil {
				t.Fatal("Stash silently dropped the requested fingerprint")
			}
		}
		if n.Protocol == "hysteria2" {
			n.TLS.ALPN = []string{"h3"}
			if _, _, err := Render([]Node{n}, Template{ID: "test", Client: "egern", Version: 1, Mode: "nodes"}, false); err == nil {
				t.Fatal("Egern silently dropped ALPN")
			}
		}
		if n.Protocol == "tuic" {
			n.UDPRelay = "quic"
			if _, _, err := Render([]Node{n}, Template{ID: "test", Client: "surge", Version: 1, Mode: "nodes"}, false); err == nil {
				t.Fatal("Surge silently dropped UDP relay mode")
			}
		}
	}
}
