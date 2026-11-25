package manage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupWaitsForPublicationAndPreservesRenewal(t *testing.T) {
	root, core, _ := appFixture(t)
	s, c, _ := platformFixture(t, root, core)
	locked, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- withStartupLock(t.Context(), root, func() error { close(locked); <-release; return nil })
	}()
	<-locked
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	_, err := s.Backup(ctx, []byte("backup-consistency-password"))
	cancel()
	close(release)
	if err == nil {
		t.Fatal("backup ignored publication lock")
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	asset := CertificateAsset{ID: "managed", Provider: "acme", Domain: "example.com", Email: "test@example.com", Certificate: c.App.fixtureProfile().Certificate, Key: c.App.fixtureProfile().Key}
	c.Certificates = append(c.Certificates, asset)
	b := platformBackup{Files: map[string]string{}}
	b.Root = root
	for _, path := range certificatePaths(c) {
		rel, e := filepath.Rel(root, path)
		if e != nil {
			t.Fatal(e)
		}
		b.Files[filepath.ToSlash(rel)] = path
	}
	keyRel, err := filepath.Rel(root, c.App.fixtureProfile().Key)
	if err != nil {
		t.Fatal(err)
	}
	keyName := filepath.ToSlash(keyRel)
	originalKey := b.Files[keyName]
	badKey := filepath.Join(t.TempDir(), "invalid.key")
	if err = os.WriteFile(badKey, []byte("invalid private key"), 0600); err != nil {
		t.Fatal(err)
	}
	b.Files[keyName] = badKey
	if err = validateArchivedPairs(c, b); err == nil {
		t.Fatal("invalid restored pair accepted")
	}
	b.Files[keyName] = originalKey
	destination := filepath.Join(t.TempDir(), "certificates", "restored")
	if err = relocateBackup(&c, b, destination); err != nil {
		t.Fatal(err)
	}
	a := c.Certificates[len(c.Certificates)-1]
	if a.Provider != "acme" || a.Email != asset.Email {
		t.Fatal("renewal ownership lost", a)
	}
	bad := a
	bad.Key = filepath.Join(t.TempDir(), "key.pem")
	if managedCertificatePair(destination, bad) {
		t.Fatal("accepted external key")
	}
}

func TestRestoredCertificateQueuesRegistration(t *testing.T) {
	root, core, _ := appFixture(t)
	s, c, _ := platformFixture(t, root, core)
	c.Certificates = []CertificateAsset{{ID: "renew", Provider: "acme", Domain: c.App.fixtureProfile().TLSSNI, Email: "test@example.com", Certificate: c.App.fixtureProfile().Certificate, Key: c.App.fixtureProfile().Key}}
	id, err := s.Store.Queue(t.Context(), "apply", "test", []byte(`{}`), 1, mustJSON(t, c))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Applied(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Finish(t.Context(), id, nil); err != nil {
		t.Fatal(err)
	}
	status, err := s.CertificateStatus(t.Context())
	if err != nil || status[0].RenewalState != "registration_pending" {
		t.Fatal(status, err)
	}
	if err = s.scheduleCertificates(t.Context()); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.Store.RecentTaskSummaries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, task := range tasks {
		if task.Kind == "certificate-renew" && task.State == "queued" {
			found = true
		}
	}
	if !found {
		t.Fatal("restored certificate with valid PEM but no lineage was not registered")
	}
	if err = s.scheduleCertificates(t.Context()); err != nil {
		t.Fatal(err)
	}
	next, err := s.Store.RecentTaskSummaries(t.Context())
	if err != nil || len(next) != len(tasks) {
		t.Fatal("registration retry storm", err)
	}
}
