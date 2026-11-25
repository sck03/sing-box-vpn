package manage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
func TestCertificateScheduleSurvivesFailedUnrelatedApply(t *testing.T) {
	root, core, _ := appFixture(t)
	s, c, _ := platformFixture(t, root, core)
	c.Certificates = []CertificateAsset{{ID: "live", Provider: "acme", Domain: "live.example.test"}}
	raw, _ := encodePlatform(c)
	state, err := s.Store.Revisions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.Store.Queue(t.Context(), "apply", "test", []byte(`{}`), state.Revision, raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Applied(t.Context(), state.Revision+1); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Finish(t.Context(), id, nil); err != nil {
		t.Fatal(err)
	}
	// A failed desired change must not suppress or replace the live registry.
	c.Certificates = []CertificateAsset{{ID: "pending", Provider: "acme", Domain: "pending.example.test"}}
	raw, _ = encodePlatform(c)
	id, err = s.Store.Queue(t.Context(), "apply", "test", []byte(`{}`), state.Revision+1, raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Finish(t.Context(), id, errors.New("activation failed")); err != nil {
		t.Fatal(err)
	}
	if err := s.scheduleCertificates(t.Context()); err != nil {
		t.Fatal(err)
	}
	task, err := s.Store.ClaimLane(t.Context(), "control")
	if err != nil {
		t.Fatal(err)
	}
	var input taskInput
	if err = json.Unmarshal(task.Input, &input); err != nil {
		t.Fatal(err)
	}
	if input.Operation != "certificate-renew" || input.ID != "live" {
		t.Fatal("renewed unconfirmed registry", input)
	}
}
