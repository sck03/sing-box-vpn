package manage

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestCredentialPublicationRetryDoesNotRepeatRotation(t *testing.T) {
	runtimeBinary := os.Getenv("MANAGED_CORE_BINARY")
	if runtimeBinary == "" {
		t.Skip("set MANAGED_CORE_BINARY for runtime publication recovery")
	}
	root, core, _ := appFixture(t)
	s, _, _ := platformFixture(t, root, core)
	ctx := t.Context()
	if err := s.Accounts.SaveAccount(ctx, domain.Account{ID: "a", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	c := domain.Credential{ID: "credential", AccountID: "a", RuntimeID: "normal", InboundID: "vless-reality", Protocol: "vless", Generation: 1, UUID: randomUUID(), Password: randomSecret()}
	if err := s.Accounts.BindBatch(ctx, []domain.Credential{c}); err != nil {
		t.Fatal(err)
	}
	command := application.AccountCommand{Operation: "rotate", ID: c.ID, Generation: 1, Scope: "normal"}
	id, err := s.Queue(ctx, application.TaskCommand{Operation: "account", Account: command}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.Runtime = filepath.Join(root, "missing-runtime")
	task, err := s.Store.ClaimLane(ctx, domain.ControlLane)
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != id || len(task.Input) == 0 {
		t.Fatal("rotation task was not claimed with its input", task.ID)
	}
	if err = s.runAccountBatch(ctx, []controlstore.ClaimedTask{task}); err != nil {
		t.Fatal(err)
	}
	finished, err := s.Store.Task(ctx, id)
	if err != nil || finished.State != "failed" || !strings.Contains(finished.Error, "credential publication incomplete") {
		t.Fatal("expected publication failure after committed rotation", finished, err)
	}
	users, revision, err := s.Accounts.CredentialSnapshot(ctx, "normal")
	if err != nil || revision != 2 || len(users) != 1 || users[0].Generation != 2 {
		t.Fatal("rotation must commit before publication fails", users, revision, err)
	}
	s.Runtime = runtimeBinary
	for range 2 {
		if err = s.reconcileCredentials(ctx); err != nil {
			t.Fatal(err)
		}
	}
	users, revision, err = s.Accounts.CredentialSnapshot(ctx, "normal")
	if err != nil || revision != 2 || len(users) != 1 || users[0].Generation != 2 {
		t.Fatal(users, revision, err)
	}
	versions, _ := s.Accounts.CredentialPublications(ctx)
	if versions[0].Published != revision {
		t.Fatal("publication never converged", versions)
	}
}

func TestCorruptedSubscriptionCacheIsQuarantined(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, subscriptionsRuntime)
	if err := AtomicBytes(path, []byte(`{"sources":broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := recoverSubscriptionRuntime(root); err != nil {
		t.Fatal(err)
	}
	if current, err := loadSubscriptionRuntime(root); err != nil || len(current.Runs) != 1 || current.Runs[0].Operation != "cache-recovery" {
		t.Fatal(current, err)
	}
	if _, err := os.Stat(filepath.Join(root, "subscriptions/runtime.corrupt.json")); err != nil {
		t.Fatal(err)
	}
	// Use an independent fixture instead of replacing a just-deleted file with
	// a directory (Windows scanners can retain a delete-pending file handle).
	unreadableRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(unreadableRoot, subscriptionsRuntime), 0700); err != nil {
		t.Fatal(err)
	}
	if err := recoverSubscriptionRuntime(unreadableRoot); err == nil {
		t.Fatal("I/O failure silently ignored")
	}
}

func TestRestoreTemporaryFileRemovedAfterInvalidRequest(t *testing.T) {
	v := backupRequest{}
	r := multipartRestoreRequest(t, []byte("encrypted"), map[string]string{})
	if err := readBackupRequest(httptest.NewRecorder(), r, &v); err == nil {
		t.Fatal("missing password accepted")
	}
	if v.Backup == nil {
		t.Fatal("fixture did not spool upload")
	}
	name := v.Backup.Name()
	v.Close()
	if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("encrypted spool leaked", err)
	}
}

func TestCountryNormalizesNullMapsAndBuildIgnoresBrokenProjections(t *testing.T) {
	root, core, _ := appFixture(t)
	s, c, _ := platformFixture(t, root, core)
	c.VPN.Profiles = nil
	c.VPNSecrets = nil
	c.App.Name = "unsaved-country-draft"
	if _, err := s.Store.Queue(t.Context(), "apply", "test", []byte(`{}`), 1, mustJSON(t, c)); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareCountry(s.Root, c, "JP")
	if err != nil || len(prepared.VPN.Profiles) != 1 || len(prepared.VPNSecrets) != 1 {
		t.Fatal(prepared.VPN, err)
	}
	if prepared.App.Name != "unsaved-country-draft" {
		t.Fatal("country preparation discarded draft")
	}
	for _, path := range []string{subscriptionsConfig, subscriptionsSnapshot} {
		if err = os.WriteFile(filepath.Join(root, path), []byte("broken"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.build(t.Context(), c); err != nil {
		t.Fatal("projection prevented recovery", err)
	}
}

func TestCertificateGarbageCollectionProtectsReferencesAndGrace(t *testing.T) {
	root, core, _ := appFixture(t)
	s, c, _ := platformFixture(t, root, core)
	now := time.Now()
	for _, name := range []string{"keep", "panel", "old", "draft", "unowned"} {
		dir := filepath.Join(root, "certificates", name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if name != "unowned" {
			p := filepath.Join(dir, ".managed-material")
			if err := os.WriteFile(p, []byte("owned"), 0600); err != nil {
				t.Fatal(err)
			}
			if name != "draft" {
				old := now.Add(-8 * 24 * time.Hour)
				os.Chtimes(p, old, old)
			}
		}
	}
	c.Certificates = append(c.Certificates, CertificateAsset{ID: "keep", Certificate: filepath.Join(root, "certificates/keep/cert.pem"), Key: filepath.Join(root, "certificates/keep/key.pem")})
	if err := AtomicJSON(filepath.Join(root, "panel.json"), PanelOptions{Listen: "127.0.0.1:2096", Certificate: filepath.Join(root, "certificates/panel/cert.pem"), Key: filepath.Join(root, "certificates/panel/key.pem")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.Queue(t.Context(), "apply", "test", []byte(`{}`), 1, mustJSON(t, c)); err != nil {
		t.Fatal(err)
	}
	if err := s.pruneCertificates(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"keep", "panel", "draft", "unowned"} {
		if _, err := os.Stat(filepath.Join(root, "certificates", name)); err != nil {
			t.Fatal(name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "certificates/old")); !os.IsNotExist(err) {
		t.Fatal("orphan retained", err)
	}
}
