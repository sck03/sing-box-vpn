package manage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func coreBinary(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("SING_BOX_BINARY")
	if binary == "" {
		t.Skip("set SING_BOX_BINARY to a verified sing-box binary")
	}
	return binary
}
func appFixture(t *testing.T) (string, string, App) {
	t.Helper()
	root, core := t.TempDir(), coreBinary(t)
	p, err := NewProfile(root, map[int]bool{}, "")
	if err != nil {
		t.Fatal(err)
	}
	app := App{Name: "test", Address: "127.0.0.1", Profile: p, Inbounds: MakeInbounds(p, ""), Routing: map[string][]string{}, SocksPort: 40000}
	if err = CommitApp(context.Background(), root, core, &app); err != nil {
		t.Fatal(err)
	}
	return root, core, app
}
func readObject(t *testing.T, path string) Object {
	t.Helper()
	var value Object
	found, err := ReadJSON(path, &value)
	if err != nil || !found {
		t.Fatal("read config", err)
	}
	return value
}
func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestNativeRealityMatchesRFC7748(t *testing.T) {
	private, _ := hex.DecodeString("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	public, err := derivePublic(base64.RawURLEncoding.EncodeToString(private))
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := hex.DecodeString("8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a")
	if public != base64.RawURLEncoding.EncodeToString(expected) {
		t.Fatal("X25519 migration changed the public key")
	}
}
func TestCertificateRejectsMismatchedAndMissingKeys(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	cert, key := filepath.Join(a, "cert.pem"), filepath.Join(a, "key.pem")
	if err := EnsureCertificate(cert, key, "www.bing.com"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCertificate(filepath.Join(b, "cert.pem"), filepath.Join(b, "key.pem"), "www.bing.com"); err != nil {
		t.Fatal(err)
	}
	before := readBytes(t, cert)
	if err := EnsureCertificate(cert, filepath.Join(b, "key.pem"), "www.bing.com"); err == nil {
		t.Fatal("accepted a certificate from a different key")
	}
	if !bytes.Equal(before, readBytes(t, cert)) {
		t.Fatal("mismatch replaced the certificate")
	}
	if err := os.Remove(key); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCertificate(cert, key, "www.bing.com"); err == nil {
		t.Fatal("silently replaced a missing key")
	}
}

func TestFiveConfigsValidateWithCurrentStableCore(t *testing.T) {
	root, _, _ := appFixture(t)
	client := readObject(t, filepath.Join(root, "sbox.json"))
	kinds := map[string]bool{}
	for _, node := range objects(client["outbounds"]) {
		if Labels[text(node["type"])] != "" {
			kinds[text(node["type"])] = true
		}
	}
	if len(kinds) != 5 {
		t.Fatal("missing protocols", kinds)
	}
	if object(readObject(t, filepath.Join(root, "sb.json"))["route"])["final"] != "direct" {
		t.Fatal("ordinary exit changed")
	}
}
func TestCountryLinksStayStableAndCredentialsStayIndependent(t *testing.T) {
	root, core, app := appFixture(t)
	ctx := context.Background()
	ordinary := readBytes(t, filepath.Join(root, "sb.json"))
	ordinaryLinks := readBytes(t, filepath.Join(root, "jhsub.txt"))
	if _, err := PrepareVPN(ctx, root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	first := readBytes(t, filepath.Join(root, "vpngate", "jhsub.txt"))
	if _, err := PrepareVPN(ctx, root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, readBytes(t, filepath.Join(root, "vpngate", "jhsub.txt"))) {
		t.Fatal("same-country links changed")
	}
	if _, err := PrepareVPN(ctx, root, core, app, "KR"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ordinary, readBytes(t, filepath.Join(root, "sb.json"))) {
		t.Fatal("country selection changed ordinary nodes")
	}
	if !bytes.Equal(ordinaryLinks, readBytes(t, filepath.Join(root, "jhsub.txt"))) {
		t.Fatal("VPNGate selection changed ordinary subscriptions")
	}
	entries := objects(readObject(t, filepath.Join(root, "vpngate", "server.json"))["inbounds"])
	if len(entries) != 5 {
		t.Fatal("inactive countries kept listeners", len(entries))
	}
	for _, entry := range entries {
		if !strings.HasPrefix(text(entry["tag"]), "vg-KR-") {
			t.Fatal("inactive country retained an entry")
		}
	}
	_, private, err := loadVPN(root)
	if err != nil {
		t.Fatal(err)
	}
	if private["JP"].SocksPass == private["KR"].SocksPass {
		t.Fatal("countries share credentials")
	}
	gateway := readObject(t, filepath.Join(root, "vpngate", "gateway.json"))
	if gateway["country"] != "KR" || gateway["proxy_user"] != private["KR"].SocksUser {
		t.Fatal("wrong active country")
	}
	if _, err := PrepareVPN(ctx, root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, readBytes(t, filepath.Join(root, "vpngate", "jhsub.txt"))) {
		t.Fatal("returning to a saved country changed its ports or credentials")
	}
	for _, out := range objects(readObject(t, filepath.Join(root, "sbox.json"))["outbounds"]) {
		if out["tag"] == "auto" {
			for _, tag := range stringsValue(out["outbounds"]) {
				if strings.Contains(tag, "VPNGate") {
					t.Fatal("gateway entered automatic selection")
				}
			}
		}
	}
}

func TestInvalidClientDoesNotCommitAnyWorkingConfig(t *testing.T) {
	root, core, app := appFixture(t)
	files := []string{"sb.json", "app.json", "sbox.json", "clmi.yaml", "jhsub.txt"}
	before := map[string][]byte{}
	for _, name := range files {
		before[name] = readBytes(t, filepath.Join(root, name))
	}
	bad := app
	bad.Profile.Secrets.Reality.Public = "invalid-public-key"
	if err := CommitApp(context.Background(), root, core, &bad); err == nil {
		t.Fatal("invalid client config was accepted")
	}
	for _, name := range files {
		if !bytes.Equal(before[name], readBytes(t, filepath.Join(root, name))) {
			t.Fatalf("failed validation replaced %s", name)
		}
	}
}
func TestImportPreservesPortsUsersAndRealityKey(t *testing.T) {
	root, core, app := appFixture(t)
	original := readObject(t, filepath.Join(root, "sb.json"))
	objects(original["inbounds"])[0]["sniff"] = true
	if err := AtomicJSON(filepath.Join(root, "sb.json"), original); err != nil {
		t.Fatal(err)
	}
	p, imported, routing, warp, err := ImportExisting(root)
	if err != nil {
		t.Fatal(err)
	}
	for i, old := range objects(original["inbounds"]) {
		if !reflect.DeepEqual(old["users"], imported[i]["users"]) || old["listen_port"] != imported[i]["listen_port"] {
			t.Fatal("migration changed port or credentials")
		}
	}
	if _, exists := imported[0]["sniff"]; exists {
		t.Fatal("obsolete option retained")
	}
	if p.Secrets.Reality.Public != app.Profile.Secrets.Reality.Public {
		t.Fatal("Reality public key changed")
	}
	server, err := ServerConfig(imported, routing, warp, 40000)
	if err != nil {
		t.Fatal(err)
	}
	if err = CheckJSON(context.Background(), core, root, server); err != nil {
		t.Fatal(err)
	}
}
func TestHoppingAndMihomoManualGroup(t *testing.T) {
	root, core, app := appFixture(t)
	app.Profile.Hopping = []string{"31000:31100", "31200"}
	if err := CommitApp(context.Background(), root, core, &app); err != nil {
		t.Fatal(err)
	}
	client := readObject(t, filepath.Join(root, "sbox.json"))
	for _, node := range objects(client["outbounds"]) {
		if node["type"] == "hysteria2" && !reflect.DeepEqual(stringsValue(node["server_ports"]), []string{"31000:31100", "31200:31200"}) {
			t.Fatal("invalid hopping format")
		}
	}
	if _, err := PrepareVPN(context.Background(), root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	mihomo := readObject(t, filepath.Join(root, "vpngate", "clmi.yaml"))
	found := false
	for _, group := range objects(mihomo["proxy-groups"]) {
		if group["name"] == "临时家宽" {
			found = true
			if group["type"] != "select" {
				t.Fatal("gateway group is not manual")
			}
			for _, name := range stringsValue(group["proxies"]) {
				if name == "DIRECT" {
					t.Fatal("gateway group contains direct exit")
				}
			}
		}
	}
	if !found {
		t.Fatal("VPNGate subscription has no manual group")
	}
}
func TestPinsFollowIndividualProtocolCertificates(t *testing.T) {
	root, core, app := appFixture(t)
	other := t.TempDir()
	cert, key := filepath.Join(other, "cert.pem"), filepath.Join(other, "key.pem")
	if err := EnsureCertificate(cert, key, "example.com"); err != nil {
		t.Fatal(err)
	}
	if err := mutateApp(&app, VPNSettings{}, "tls", []string{"hysteria2", cert, key, "example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := CommitApp(context.Background(), root, core, &app); err != nil {
		t.Fatal(err)
	}
	cache := certificateCache{}
	oldInfo, err := cache.get(app.Profile.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	newInfo, err := cache.get(cert)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range objects(readObject(t, filepath.Join(root, "sbox.json"))["outbounds"]) {
		kind := text(node["type"])
		if kind != "hysteria2" && kind != "anytls" {
			continue
		}
		expected := oldInfo.Pin
		if kind == "hysteria2" {
			expected = newInfo.Pin
		}
		pins := stringsValue(object(node["tls"])["certificate_public_key_sha256"])
		if len(pins) != 1 || pins[0] != expected {
			t.Fatal("certificate change affected another protocol", kind)
		}
	}
}
func TestLegacyFixtureIsPreserved(t *testing.T) {
	root := os.Getenv("LEGACY_FIXTURE")
	if root == "" {
		t.Skip("set LEGACY_FIXTURE for migration comparison")
	}
	core := coreBinary(t)
	before := map[string][]byte{}
	for _, name := range []string{"app.json", "vpngate/settings.json", "vpngate/secrets.json"} {
		if data, err := os.ReadFile(filepath.Join(root, name)); err == nil {
			before[name] = data
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if err := RunConfig(context.Background(), []string{"--root", root, "--core", core, "export"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for name, data := range before {
		if !bytes.Equal(data, readBytes(t, filepath.Join(root, name))) {
			t.Fatalf("export changed legacy state %s", name)
		}
	}
	expected := map[string]bool{}
	for _, link := range strings.Split(strings.TrimSpace(string(readBytes(t, filepath.Join(root, "expected", "jhsub.txt")))), "\n") {
		expected[link] = true
	}
	settings, _, err := loadVPN(root)
	if err != nil {
		t.Fatal(err)
	}
	share := readObject(t, filepath.Join(root, "expected", "share.json"))
	for _, scope := range []string{normalScope, vpngateScope} {
		if scope == vpngateScope && settings.ActiveCountry == "" {
			continue
		}
		directory, _ := exportDirectory(root, scope)
		links := strings.Split(strings.TrimSpace(string(readBytes(t, filepath.Join(directory, "jhsub.txt")))), "\n")
		want := len(stringsValue(share["normal"]))
		if scope == vpngateScope {
			want = len(Protocols)
		}
		if len(links) != want {
			t.Fatalf("legacy %s lost nodes: got %d, want %d", scope, len(links), want)
		}
		for _, link := range links {
			if !expected[link] {
				t.Fatalf("legacy %s URI changed", scope)
			}
		}
	}
}

func TestBashFacingConfigurationCommands(t *testing.T) {
	root, core, _ := appFixture(t)
	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := RunConfig(context.Background(), append([]string{"--root", root, "--core", core}, args...), &output); err != nil {
			t.Fatal(args[0], err)
		}
		return output.String()
	}
	run("cdn", "cdn.example.com")
	run("cdn", "")
	if text(readObject(t, filepath.Join(root, "app.json"))["cdn_address"]) != "" {
		t.Fatal("menu could not clear CDN address")
	}
	run("subscription-init", "--port", "0")
	if len(strings.Split(strings.TrimSpace(run("subscription-urls")), "\n")) != 3 {
		t.Fatal("missing subscription URLs")
	}
	run("subscription-stop")
	if readObject(t, filepath.Join(root, "subscription.json"))["enabled"] != false {
		t.Fatal("subscription stop did not persist")
	}
	if !strings.HasSuffix(strings.TrimSpace(run("vmess-info")), "http") {
		t.Fatal("Argo menu received wrong VMess scheme")
	}
	if err := os.WriteFile(filepath.Join(root, "argo.log"), []byte("https://previous.trycloudflare.com\nhttps://current.trycloudflare.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("argo-watch", "--timeout", "5s")
	if object(readObject(t, filepath.Join(root, "app.json"))["argo"])["domain"] != "current.trycloudflare.com" {
		t.Fatal("Argo watcher did not select the latest domain")
	}
}

func BenchmarkNativeCertificatePins(b *testing.B) {
	root := b.TempDir()
	cert, key := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
	if err := EnsureCertificate(cert, key, "www.bing.com"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache := certificateCache{}
		info, err := cache.get(cert)
		if err != nil {
			b.Fatal(err)
		}
		if info.Pin == "" {
			b.Fatal("empty pin")
		}
	}
}
