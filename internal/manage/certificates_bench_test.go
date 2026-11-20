package manage

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/go-acme/lego/v5/acme"
	"github.com/go-acme/lego/v5/lego"
)

func BenchmarkCertificateStatus(b *testing.B) {
	core := os.Getenv("SING_BOX_BINARY")
	if core == "" {
		b.Skip("set SING_BOX_BINARY to a verified sing-box binary")
	}
	root := b.TempDir()
	s, err := OpenPlatform(root, core, core, true)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })
	if err = s.Initialize(b.Context(), "127.0.0.1", "certificate-bench", "admin", "benchmark-only-password"); err != nil {
		b.Fatal(err)
	}
	state, err := s.Store.State(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	config, err := decodePlatform(state.Applied)
	if err != nil {
		b.Fatal(err)
	}
	cert, err := os.ReadFile(config.App.fixtureProfile().Certificate)
	if err != nil {
		b.Fatal(err)
	}
	key, err := os.ReadFile(config.App.fixtureProfile().Key)
	if err != nil {
		b.Fatal(err)
	}
	const email = "benchmark@example.test"
	account := acmeAccount{Directory: lego.DirectoryURLLetsEncrypt, Email: email, KeyPEM: key,
		Registration: &acme.ExtendedAccount{Location: "https://acme.example.test/accounts/bench"}}
	if err = AtomicJSON(acmeAccountPath(root, account.Directory, email), account); err != nil {
		b.Fatal(err)
	}
	config.Certificates = nil
	for i := 0; i < 32; i++ {
		asset := CertificateAsset{ID: fmt.Sprintf("cert-%02d", i), Provider: "acme", Domain: config.App.fixtureProfile().TLSSNI,
			Email: email, Certificate: config.App.fixtureProfile().Certificate, Key: config.App.fixtureProfile().Key}
		config.Certificates = append(config.Certificates, asset)
		record := acmeCertificateRecord{Directory: account.Directory, Domain: asset.Domain, Email: email,
			AccountURL: account.Registration.Location, Certificate: cert, PrivateKey: key}
		if err = AtomicJSON(acmeCertificatePath(root, asset.ID), record); err != nil {
			b.Fatal(err)
		}
	}
	data, err := json.Marshal(config)
	if err != nil {
		b.Fatal(err)
	}
	task, err := s.Store.Queue(b.Context(), "apply", "benchmark", []byte("{}"), state.Revision, data)
	if err != nil {
		b.Fatal(err)
	}
	if err = s.Store.Applied(b.Context(), state.Revision+1); err != nil {
		b.Fatal(err)
	}
	if err = s.Store.Finish(b.Context(), task, nil); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		status, err := s.CertificateStatus(b.Context())
		if err != nil || len(status) != 32 {
			b.Fatalf("certificate status: count=%d, error=%v", len(status), err)
		}
	}
}
