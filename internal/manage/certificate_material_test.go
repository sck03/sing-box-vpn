package manage

import (
	"github.com/sck03/sing-box-vpn/internal/certificates"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCertificateMaterialValidation(t *testing.T) {
	root := t.TempDir()
	certPath, keyPath := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
	if err := EnsureCertificate(certPath, keyPath, "material.example.test"); err != nil {
		t.Fatal(err)
	}
	asset := CertificateAsset{Domain: "material.example.test", Certificate: certPath, Key: keyPath}
	for _, debug := range []string{"x509keypairleaf=1", "x509keypairleaf=0"} {
		t.Setenv("GODEBUG", debug)
		material, err := certificates.Read(asset)
		if err != nil {
			t.Fatal(err)
		}
		if err = material.ValidAt(time.Now()); err != nil {
			t.Fatal(err)
		}
		if material.ValidAt(material.Leaf.NotBefore.Add(-time.Second)) == nil ||
			material.ValidAt(material.Leaf.NotAfter) == nil {
			t.Fatal("certificate validity boundary was ignored")
		}
		if _, err = certificates.Parse(material.Certificate, material.Key, "different.example.test"); err == nil {
			t.Fatal("accepted an unrelated hostname")
		}
		if _, err = certificates.Parse(material.Certificate, []byte("invalid"), asset.Domain); err == nil {
			t.Fatal("accepted an invalid private key")
		}
	}
	if err := os.Rename(keyPath, keyPath+".saved"); err != nil {
		t.Fatal(err)
	}
	if _, err := certificates.Read(asset); err == nil {
		t.Fatal("accepted missing private key")
	}
}

func TestCertificateProvidersRejectLegacy(t *testing.T) {
	root, core, _ := appFixture(t)
	_, config, _ := platformFixture(t, root, core)
	config.Certificates = []CertificateAsset{{ID: "certificate", Domain: config.App.fixtureProfile().TLSSNI,
		Certificate: config.App.fixtureProfile().Certificate, Key: config.App.fixtureProfile().Key, Email: "test@example.test"}}
	for _, provider := range []string{"acme", "import", "generated"} {
		config.Certificates[0].Provider = provider
		if err := config.Validate(); err != nil {
			t.Fatal("current provider was rejected", provider, err)
		}
	}
	for _, provider := range []string{"certbot", "unknown", ""} {
		config.Certificates[0].Provider = provider
		if err := config.Validate(); err == nil {
			t.Fatal("accepted an obsolete or invalid provider", provider)
		}
	}
}
