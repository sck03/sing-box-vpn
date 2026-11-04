package manage

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestTextStatusUsesSeparateVPNPortsAndHidesSecrets(t *testing.T) {
	profile := Profile{
		Ports:      map[string]int{"vless": 21001, "vmess": 21002, "hysteria2": 21003, "tuic": 21004, "anytls": 21005},
		RealitySNI: "www.example.com",
		Secrets: Secrets{UUID: map[string]string{"vless": "private-uuid", "vmess": "private-vmess"},
			Passwords: map[string]string{"hysteria2": "private-password"}, Reality: Reality{Private: "private-reality-key"}},
	}
	app := App{Address: "203.0.113.10", Profile: profile, Inbounds: MakeInbounds(profile, ""),
		Warp: Object{"private_key": "private-warp-key"}, Routing: map[string][]string{"warp": {"example.net"}}}
	vpnProfile := profile
	vpnProfile.Ports = map[string]int{"vless": 31001, "vmess": 31002, "hysteria2": 31003, "tuic": 31004, "anytls": 31005}
	settings := VPNSettings{ActiveCountry: "JP", Profiles: map[string]Profile{"JP": vpnProfile}}
	for _, scope := range []string{normalScope, vpngateScope} {
		var output bytes.Buffer
		if err := writeStatusText(&output, app, settings, scope, "1.14.4"); err != nil {
			t.Fatal(err)
		}
		status := output.String()
		for _, forbidden := range []string{"private-", "\x1b", "\"protocols\""} {
			if strings.Contains(status, forbidden) {
				t.Fatalf("status leaked sensitive or machine-only data: %q", status)
			}
		}
		if scope == normalScope {
			if !strings.Contains(status, "21001") || strings.Contains(status, "31001") || !strings.Contains(status, "example.net") {
				t.Fatal("ordinary status mixed with VPNGate", status)
			}
		} else if !strings.Contains(status, "31001") || strings.Contains(status, "21001") || !strings.Contains(status, "日本 / JP") || strings.Contains(status, "example.net") {
			t.Fatal("VPNGate status mixed with ordinary nodes", status)
		}
	}
}

func TestTextStatusRemainsAvailableWhenCoreIsMissing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.json")
	app := App{Address: "203.0.113.10\n\x1b[31m", Profile: Profile{RealitySNI: "example.com\r\u202e"}}
	if err := AtomicJSON(path, app); err != nil {
		t.Fatal(err)
	}
	before := readBytes(t, path)
	var output bytes.Buffer
	if err := RunConfig(context.Background(), []string{"--root", root, "show", "--format", "text"}, &output); err != nil {
		t.Fatal(err)
	}
	status := output.String()
	if !strings.Contains(status, "不可用") || strings.ContainsAny(status, "\x1b\r\u202e") || !strings.Contains(status, "VLESS-Reality") {
		t.Fatal("missing-core status is not readable", status)
	}
	if !bytes.Equal(before, readBytes(t, path)) {
		t.Fatal("reading status changed the configuration")
	}
	// Machine callers keep the original error behavior and never receive text as JSON.
	output.Reset()
	if err := RunConfig(context.Background(), []string{"--root", root, "show"}, &output); err == nil || output.Len() != 0 {
		t.Fatal("default JSON status behavior changed", err, output.String())
	}
}
