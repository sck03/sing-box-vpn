package manage

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"github.com/sck03/sing-box-vpn/internal/certificates"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"go.yaml.in/yaml/v3"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func coreBinary(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("SING_BOX_BINARY")
	if binary == "" {
		t.Skip("set SING_BOX_BINARY to a verified sing-box binary")
	}
	return binary
}
func appFixture(t *testing.T, excluded ...portRange) (string, string, App) {
	t.Helper()
	root, core := t.TempDir(), coreBinary(t)
	p, err := NewProfile(root, map[int]bool{}, "", excluded...)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Name: "test", Address: "127.0.0.1", Nodes: MakeNodes(p, "")}
	if err = publishFixtureApp(context.Background(), root, core, &app); err != nil {
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

func TestTLSImportRejectsWrongDomainAndMissingFiles(t *testing.T) {
	root := t.TempDir()
	cert, key := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
	if err := EnsureCertificate(cert, key, "example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := certificates.Read(CertificateAsset{Certificate: cert, Key: key, Domain: "other.example"}); err == nil {
		t.Fatal("TLS import accepted a certificate that does not cover its SNI")
	}
	missingCert, missingKey := filepath.Join(root, "missing.pem"), filepath.Join(root, "missing.key")
	if _, err := certificates.Read(CertificateAsset{Certificate: missingCert, Key: missingKey, Domain: "example.com"}); err == nil {
		t.Fatal("TLS import silently generated a self-signed certificate at a mistyped path")
	}
	for _, path := range []string{missingCert, missingKey} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("failed import created files", err)
		}
	}
}

func TestTLSImportRejectsInvalidValidityPeriod(t *testing.T) {
	for _, future := range []bool{false, true} {
		root := t.TempDir()
		certPath, keyPath := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
		if err := EnsureCertificate(certPath, keyPath, "example.com"); err != nil {
			t.Fatal(err)
		}
		pair, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			t.Fatal(err)
		}
		cert, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			t.Fatal(err)
		}
		cert.NotBefore, cert.NotAfter = time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour)
		if future {
			cert.NotBefore, cert.NotAfter = time.Now().Add(time.Hour), time.Now().Add(2*time.Hour)
		}
		der, err := x509.CreateCertificate(rand.Reader, cert, cert, cert.PublicKey, pair.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := AtomicBytes(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := certificates.ValidatePEM(readBytes(t, certPath), readBytes(t, keyPath), "example.com"); err == nil {
			t.Fatal("expired or not-yet-valid certificate was accepted")
		}
	}
}

func TestFiveConfigsValidateWithCurrentStableCore(t *testing.T) {
	root, _, _ := appFixture(t)
	client := readArtifactObject(t, root, "normal-sing-box")
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
	ordinaryLinks := readArtifact(t, root, "normal-uri")
	if _, err := prepareFixtureCountry(ctx, root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	first := readArtifact(t, root, "vpngate-uri")
	if _, err := prepareFixtureCountry(ctx, root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, readArtifact(t, root, "vpngate-uri")) {
		t.Fatal("same-country links changed")
	}
	if _, err := prepareFixtureCountry(ctx, root, core, app, "KR"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ordinary, readBytes(t, filepath.Join(root, "sb.json"))) {
		t.Fatal("country selection changed ordinary nodes")
	}
	if !bytes.Equal(ordinaryLinks, readArtifact(t, root, "normal-uri")) {
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
	if _, err := prepareFixtureCountry(ctx, root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, readArtifact(t, root, "vpngate-uri")) {
		t.Fatal("returning to a saved country changed its ports or credentials")
	}
	for _, out := range objects(readArtifactObject(t, root, "normal-sing-box")["outbounds"]) {
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
	files := []string{"sb.json", "app.json", subscriptionsSnapshot}
	before := map[string][]byte{}
	for _, name := range files {
		before[name] = readBytes(t, filepath.Join(root, name))
	}
	bad := app
	bad.Nodes[0].TLS.RealityPrivate = "invalid-private-key"
	if err := publishFixtureApp(context.Background(), root, core, &bad); err == nil {
		t.Fatal("invalid client config was accepted")
	}
	for _, name := range files {
		if !bytes.Equal(before[name], readBytes(t, filepath.Join(root, name))) {
			t.Fatalf("failed validation replaced %s", name)
		}
	}
}
func TestHoppingAndMihomoManualGroup(t *testing.T) {
	root, core, app := appFixture(t, portRange{31000, 31100}, portRange{31200, 31200})
	setFixtureUDPPorts(&app, "hysteria2", []string{"31000:31100", "31200"})
	if err := publishFixtureApp(context.Background(), root, core, &app); err != nil {
		t.Fatal(err)
	}
	client := readArtifactObject(t, root, "normal-sing-box")
	for _, node := range objects(client["outbounds"]) {
		if node["type"] == "hysteria2" && !reflect.DeepEqual(stringsValue(node["server_ports"]), []string{"31000:31100", "31200:31200"}) {
			t.Fatal("invalid hopping format")
		}
	}
	if _, err := prepareFixtureCountry(context.Background(), root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	settings, secrets, err := loadVPN(root)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := fixtureSubscriptionNodes(app, settings, secrets)
	if err != nil {
		t.Fatal(err)
	}
	artifact, _, err := sub.Render(nodes[vpngateScope], sub.Template{ID: "gateway", Client: "mihomo", Version: 1, Mode: "config"}, false)
	if err != nil {
		t.Fatal(err)
	}
	var mihomo Object
	if err = yaml.Unmarshal(artifact.Data, &mihomo); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, group := range objects(mihomo["proxy-groups"]) {
		if group["name"] == "VPNGate" {
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
	other := filepath.Join(root, "custom")
	cert, key := filepath.Join(other, "cert.pem"), filepath.Join(other, "key.pem")
	if err := EnsureCertificate(cert, key, "example.com"); err != nil {
		t.Fatal(err)
	}
	for i := range app.Nodes {
		if app.Nodes[i].Protocol == "hysteria2" {
			app.Nodes[i].TLS.Certificate, app.Nodes[i].TLS.Key, app.Nodes[i].TLS.ServerName = cert, key, "example.com"
		}
	}
	if err := publishFixtureApp(context.Background(), root, core, &app); err != nil {
		t.Fatal(err)
	}
	cache := certificateCache{}
	oldInfo, err := cache.get(app.fixtureProfile().Certificate)
	if err != nil {
		t.Fatal(err)
	}
	newInfo, err := cache.get(cert)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range objects(readArtifactObject(t, root, "normal-sing-box")["outbounds"]) {
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
