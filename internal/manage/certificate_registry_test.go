package manage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCertificateRegistryPreparationAndDuplicateImport(t *testing.T) {
	root, core, _ := appFixture(t)
	s, c, _ := platformFixture(t, root, core)
	initial := initialCertificates(c.App)
	if len(initial) == 0 || initial[0].Provider != "generated" {
		t.Fatal("generated material not registered", initial)
	}
	asset, err := s.PrepareCertificate(t.Context(), CertificatePreparation{ID: "panel-cert", Domain: "panel.example.test", Email: "owner@example.test"})
	if err != nil || !managedCertificatePair(root, asset) {
		t.Fatal(asset, err)
	}
	if _, err = os.Stat(filepath.Dir(asset.Certificate)); !os.IsNotExist(err) {
		t.Fatal("prepare unexpectedly wrote material")
	}
	for _, id := range []string{"../escape", ""} {
		if _, err = s.PrepareCertificate(t.Context(), CertificatePreparation{ID: id, Domain: "panel.example.test", Email: "owner@example.test"}); err == nil {
			t.Fatal("invalid ID accepted")
		}
	}
	if _, err = s.PrepareCertificate(t.Context(), CertificatePreparation{ID: "new", Domain: "panel.example.test", Email: "invalid"}); err == nil {
		t.Fatal("invalid email accepted")
	}
	c.Certificates = initial
	raw, _ := encodePlatform(c)
	state, err := s.Store.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.Queue(t.Context(), "apply", "test", []byte(`{}`), state.Revision, raw); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ImportCertificate(t.Context(), CertificateImport{ID: initial[0].ID, Domain: initial[0].Domain, Certificate: "invalid", Key: "invalid"}); err == nil {
		t.Fatal("duplicate import accepted")
	}
}
func TestCertificateScheduleDeadlineUsesAppliedRevision(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, _ := platformFixture(t, root, core)
	if err := s.scheduleCertificates(t.Context()); err != nil {
		t.Fatal(err)
	}
	deadline := s.certificateNext
	if !deadline.After(time.Now()) {
		t.Fatal("missing schedule deadline")
	}
	if err := s.scheduleCertificates(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !deadline.Equal(s.certificateNext) {
		t.Fatal("unchanged revision rescanned certificate material")
	}
	s.certificateRevision = 0
	s.certificateNext = time.Now().Add(time.Hour)
	if err := s.scheduleCertificates(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s.certificateRevision == 0 || s.certificateNext.After(time.Now().Add(11*time.Minute)) {
		t.Fatal("revision change did not invalidate deadline")
	}
}
