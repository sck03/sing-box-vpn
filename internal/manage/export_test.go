package manage

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSeparateExports(t *testing.T) {
	root, core, app := appFixture(t)
	before := map[string][]byte{}
	for _, name := range exportNames {
		before[name] = readBytes(t, filepath.Join(root, name))
	}
	if _, err := PrepareVPN(context.Background(), root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	for _, name := range exportNames {
		if !bytes.Equal(before[name], readBytes(t, filepath.Join(root, name))) {
			t.Fatalf("VPNGate generation changed ordinary %s", name)
		}
	}
	for _, scope := range []string{normalScope, vpngateScope} {
		directory, err := exportDirectory(root, scope)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := RunConfig(context.Background(), []string{"--root", root, "--core", core, "links", "--scope", scope}, &output); err != nil {
			t.Fatal(err)
		}
		links := readBytes(t, filepath.Join(directory, "jhsub.txt"))
		if !bytes.Equal(output.Bytes(), links) || len(strings.Split(strings.TrimSpace(string(links)), "\n")) != 5 {
			t.Fatalf("%s did not export exactly its five nodes", scope)
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(readBytes(t, filepath.Join(directory, "jhsub-base64.txt")))))
		if err != nil || string(decoded) != strings.TrimSuffix(string(links), "\n") {
			t.Fatal("scoped aggregate subscription changed its links", err)
		}
		client := readObject(t, filepath.Join(directory, "sbox.json"))
		protocols := map[string]bool{}
		for _, node := range objects(client["outbounds"]) {
			if Labels[text(node["type"])] != "" {
				protocols[text(node["type"])] = true
				if strings.HasPrefix(text(node["tag"]), "VPNGate-") != (scope == vpngateScope) {
					t.Fatal("ordinary and VPNGate nodes were mixed", scope)
				}
			}
			if scope == vpngateScope {
				if node["type"] == "urltest" {
					t.Fatal("VPNGate enabled automatic node switching")
				}
				for _, tag := range stringsValue(node["outbounds"]) {
					if !strings.HasPrefix(tag, "VPNGate-JP-") {
						t.Fatal("VPNGate selector contains a fallback", tag)
					}
				}
			}
		}
		if len(protocols) != 5 {
			t.Fatal("scope is missing a protocol", scope, protocols)
		}
		if scope == vpngateScope {
			for _, rule := range objects(object(client["route"])["rules"]) {
				if rule["outbound"] == "direct" && rule["ip_is_private"] != true {
					t.Fatal("VPNGate client bypasses its exit for public traffic")
				}
			}
		}
	}
}

func TestRemoveVPNKeepsOrdinaryExports(t *testing.T) {
	root, core, app := appFixture(t)
	if _, err := PrepareVPN(context.Background(), root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	ordinary := readBytes(t, filepath.Join(root, "jhsub.txt"))
	if err := RunConfig(context.Background(), []string{"--root", root, "--core", core, "vpngate-remove"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range exportNames {
		if _, err := os.Stat(filepath.Join(root, "vpngate", name)); !os.IsNotExist(err) {
			t.Fatal("uninstalled VPNGate left a usable subscription", name, err)
		}
	}
	if !bytes.Equal(ordinary, readBytes(t, filepath.Join(root, "jhsub.txt"))) {
		t.Fatal("VPNGate removal changed ordinary nodes")
	}
}

func TestCoreChecksVPNClient(t *testing.T) {
	root, core, app := appFixture(t)
	if _, err := PrepareVPN(context.Background(), root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	if err := AtomicBytes(filepath.Join(root, "vpngate", "sbox.json"), []byte(`{"invalid_field":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkConfigs(context.Background(), root, core); err == nil || !strings.Contains(err.Error(), "vpngate/sbox.json") {
		t.Fatal("core update ignored the separate VPNGate client", err)
	}
}

func TestVPNSettingsAreIndependent(t *testing.T) {
	root, core, app := appFixture(t)
	settings, err := PrepareVPN(context.Background(), root, core, app, "JP")
	if err != nil {
		t.Fatal(err)
	}
	ordinary := readBytes(t, filepath.Join(root, "sb.json"))
	ordinaryLinks := readBytes(t, filepath.Join(root, "jhsub.txt"))
	_, originalSecrets, err := loadVPN(root)
	if err != nil {
		t.Fatal(err)
	}
	old := originalSecrets["JP"]
	gatewayPath := filepath.Join(root, "vpngate", "gateway.json")
	gateway := readObject(t, gatewayPath)
	gateway["enabled"] = false
	gateway["data_dir"], gateway["run_dir"] = "/custom/data", "/custom/run"
	if err := AtomicJSON(gatewayPath, gateway); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) error {
		return RunConfig(context.Background(), append(append([]string{"--root", root, "--core", core}, args...), "--scope", vpngateScope), &bytes.Buffer{})
	}
	port, err := AvailablePort(usedPorts(app, settings))
	if err != nil {
		t.Fatal(err)
	}
	certDir := t.TempDir()
	cert, key := filepath.Join(certDir, "cert.pem"), filepath.Join(certDir, "key.pem")
	if err := EnsureCertificate(cert, key, "vpn.example.com"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"port", "tuic", strconv.Itoa(port)}, {"reset", "tuic"}, {"sni", "www.bing.com"}, {"ws", "/private-vpn"}, {"tls", "all", cert, key, "vpn.example.com"}} {
		if err := run(args...); err != nil {
			t.Fatal(args[0], err)
		}
	}
	if !bytes.Equal(ordinary, readBytes(t, filepath.Join(root, "sb.json"))) || !bytes.Equal(ordinaryLinks, readBytes(t, filepath.Join(root, "jhsub.txt"))) {
		t.Fatal("VPNGate settings changed ordinary nodes")
	}
	settings, secrets, err := loadVPN(root)
	if err != nil {
		t.Fatal(err)
	}
	p := settings.Profiles["JP"]
	if p.Ports["tuic"] != port || p.RealitySNI != "www.bing.com" || p.WSPath != "/private-vpn" || !p.VMessTLS || p.Certificate != filepath.ToSlash(cert) {
		t.Fatal("VPNGate menu settings were not saved")
	}
	next := secrets["JP"]
	if next.UUID["tuic"] == old.UUID["tuic"] || next.Passwords["tuic"] == old.Passwords["tuic"] || next.UUID["vless"] != old.UUID["vless"] || next.SocksPass != old.SocksPass {
		t.Fatal("credential reset changed the wrong protocol or internal gateway authentication")
	}
	gateway = readObject(t, gatewayPath)
	if gateway["enabled"] != false || gateway["data_dir"] != "/custom/data" || gateway["run_dir"] != "/custom/run" {
		t.Fatal("editing an entry resumed or relocated the tunnel")
	}
	before := readBytes(t, filepath.Join(root, "vpngate", "server.json"))
	if err := run("port", "tuic", strconv.Itoa(app.Profile.Ports["vless"])); err == nil {
		t.Fatal("VPNGate took an ordinary node's reserved port")
	}
	if !bytes.Equal(before, readBytes(t, filepath.Join(root, "vpngate", "server.json"))) {
		t.Fatal("rejected port change altered the server")
	}
}

func TestUpgradePrunesInactiveVPNListeners(t *testing.T) {
	root, core, app := appFixture(t)
	ctx := context.Background()
	for _, country := range []string{"JP", "KR"} {
		if _, err := PrepareVPN(ctx, root, core, app, country); err != nil {
			t.Fatal(err)
		}
	}
	settings, private, err := loadVPN(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "vpngate", "server.json")
	server := readObject(t, path)
	p := settings.Profiles["JP"]
	p.Secrets = private["JP"]
	server["inbounds"] = append(objects(server["inbounds"]), MakeInbounds(p, "vg-JP-")...)
	if err := AtomicJSON(path, server); err != nil {
		t.Fatal(err)
	}
	before := readBytes(t, filepath.Join(root, "vpngate", "jhsub.txt"))
	if err := RunConfig(ctx, []string{"--root", root, "--core", core, "init"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	entries := objects(readObject(t, path)["inbounds"])
	if len(entries) != 5 {
		t.Fatal("upgrade retained historical listeners", len(entries))
	}
	for _, entry := range entries {
		if !strings.HasPrefix(text(entry["tag"]), "vg-KR-") {
			t.Fatal("upgrade enabled an inactive country's entry")
		}
	}
	if !bytes.Equal(before, readBytes(t, filepath.Join(root, "vpngate", "jhsub.txt"))) {
		t.Fatal("upgrade changed the active country's links")
	}
}
