package manage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDeploymentPlanPreservesStoppedAndUnaffected(t *testing.T) {
	old := PlatformConfig{App: App{Nodes: []ManagedNode{}}, Argo: TunnelSettings{Mode: "off"}}
	c := old
	c.Listener.Enabled = true
	plan, err := deploymentPlan(c, old, []string{"sb-core"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan, []ServiceChange{{Name: "sb-subscription", Action: "enable-start", Reason: "enabled"}}) {
		t.Fatal(plan)
	}
	c = old
	c.App.Advanced.Log = Object{"level": "debug"}
	if p, err := deploymentPlan(c, old, nil); err != nil || len(p) != 0 {
		t.Fatal("started a stopped runtime", p)
	}
	plan, err = deploymentPlan(c, old, []string{"sb-core"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan, []ServiceChange{{Name: "sb-core", Action: "restart", Reason: "runtime-config"}}) {
		t.Fatal(plan)
	}
}
func TestEncryptedPlatformBackupDisasterLedger(t *testing.T) {
	root, core, _ := appFixture(t)
	s, c, _ := platformFixture(t, root, core)
	ctx := t.Context()
	quota := int64(10000)
	if e := s.Accounts.SaveAccount(ctx, domain.Account{ID: "a", Name: "Alice", QuotaBytes: &quota}); e != nil {
		t.Fatal(e)
	}
	consumeTestQuota(t, s, "a", 1000)
	token, e := s.Accounts.CreateGrant(ctx, accounting.Grant{ID: "g", AccountID: "a", Profiles: []string{"uri"}})
	if e != nil {
		t.Fatal(e)
	}
	c.Certificates = append(c.Certificates, CertificateAsset{ID: "pending", Domain: "example.com", Provider: "acme", Certificate: filepath.ToSlash(filepath.Join(root, "certificates/pending/cert.pem")), Key: filepath.ToSlash(filepath.Join(root, "certificates/pending/key.pem"))})
	id, e := s.Store.Queue(ctx, "apply", "test", []byte(`{}`), 1, mustJSON(t, c))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.Applied(ctx, 2); e != nil {
		t.Fatal(e)
	}
	s.Store.Finish(ctx, id, nil)
	password := []byte("backup-password-123")
	data, e := s.Backup(ctx, password)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(data, []byte("Alice")) || bytes.Contains(data, []byte(token)) {
		t.Fatal("backup plaintext")
	}
	corrupted := append([]byte(nil), data...)
	corrupted[len(corrupted)-1] ^= 1
	if _, e = decodePlatformBackup(corrupted, password); e == nil {
		t.Fatal("tampered backup accepted")
	}
	destination := filepath.Join(t.TempDir(), "recovered")
	backupPath := filepath.Join(t.TempDir(), "backup.sbvpn")
	if e = os.WriteFile(backupPath, data, 0600); e != nil {
		t.Fatal(e)
	}
	request := mustJSON(t, Object{"backup_path": backupPath, "password": string(password), "username": "newadmin", "new_password": "new-password-123"})
	if e = RunPanel(ctx, []string{"--root", destination, "restore"}, bytes.NewReader(request), new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	recovered, e := OpenPlatform(destination, core, core, true)
	if e != nil {
		t.Fatal(e)
	}
	defer recovered.Close()
	u, e := recovered.Accounts.Account(ctx, "a")
	if e != nil || u.Charged != 1000 || !u.Disabled {
		t.Fatal(u, e)
	}
	if _, _, e = recovered.Accounts.Grant(ctx, token); !errors.Is(e, accounting.ErrDenied) {
		t.Fatal("old token revived", e)
	}
	if _, e = recovered.Auth.Login(ctx, "admin", "correct-password-123", "", "local"); e == nil {
		t.Fatal("old administrator revived")
	}
	if _, e = recovered.Auth.Login(ctx, "newadmin", "new-password-123", "", "local"); e != nil {
		t.Fatal(e)
	}
	if e = RestoreDisaster(ctx, destination, data, password, "other", "new-password-123"); e == nil {
		t.Fatal("recovery overwrote live state")
	}
}
func TestReleaseExtractionRejectsEscapesLinksAndDuplicates(t *testing.T) {
	for _, name := range []string{"../escape", "sing-box-vpn/../escape", "sing-box-vpn/link", "sing-box-vpn/duplicate"} {
		t.Run(name, func(t *testing.T) {
			var raw bytes.Buffer
			gz := gzip.NewWriter(&raw)
			tw := tar.NewWriter(gz)
			h := &tar.Header{Name: name, Mode: 0644, Size: 1}
			if name == "sing-box-vpn/link" {
				h.Typeflag = tar.TypeSymlink
				h.Linkname = "/etc/passwd"
				h.Size = 0
			}
			tw.WriteHeader(h)
			if h.Size > 0 {
				tw.Write([]byte("x"))
			}
			if name == "sing-box-vpn/duplicate" {
				tw.WriteHeader(h)
				tw.Write([]byte("x"))
			}
			tw.Close()
			gz.Close()
			root := t.TempDir()
			path := filepath.Join(root, "archive.gz")
			os.WriteFile(path, raw.Bytes(), 0600)
			if e := extractProgramArchive(path, filepath.Join(root, "out")); e == nil {
				t.Fatal("unsafe release accepted")
			}
		})
	}
}
func TestAccountSourceUsesCandidatePublishingAddress(t *testing.T) {
	root, core, _ := appFixture(t)
	s, c, _ := platformFixture(t, root, core)
	ctx := t.Context()
	service := s.accountService()
	if e := s.Accounts.SaveAccount(ctx, domain.Account{ID: "a", Name: "A"}); e != nil {
		t.Fatal(e)
	}
	if e := service.Execute(ctx, application.AccountCommand{Operation: "bind", ID: "a", Scope: "normal", InboundIDs: testInboundIDs(t, service)}, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	c.App.Address = "192.0.2.44"
	service.Config = &c
	nodes, e := service.accountNodes(ctx, "a", "normal", nil)
	if e != nil || len(nodes) != 5 {
		t.Fatal(len(nodes), e)
	}
	for _, node := range nodes {
		if node.Server != "192.0.2.44" {
			t.Fatal("compiled stale address", node.Server)
		}
	}
}

func TestEffectiveServerRejectsCrossResourceCycleAndReferences(t *testing.T) {
	server := Object{"outbounds": []Object{{"type": "socks", "tag": "proxy", "domain_resolver": "dns"}}, "dns": Object{"servers": []Object{{"tag": "dns", "type": "https", "detour": "proxy"}}}}
	if e := validateEffectiveServer(server); e == nil {
		t.Fatal("DNS/outbound cycle accepted")
	}
	object(server["dns"])["servers"] = []Object{{"tag": "dns", "type": "local"}}
	if e := validateEffectiveServer(server); e != nil {
		t.Fatal(e)
	}
	server["route"] = Object{"final": "missing"}
	if e := validateEffectiveServer(server); e == nil {
		t.Fatal("missing final accepted")
	}
}
