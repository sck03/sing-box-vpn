package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/clientprofile"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestDisabledRequiredAccountDoesNotStopPlatform(t *testing.T) {
	root, core, _ := appFixture(t)
	s, c, _ := platformFixture(t, root, core)
	a := domain.Account{ID: "audit", Name: "Audit"}
	if err := s.Accounts.SaveAccount(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if err := (AccountService{Root: root, Store: s.Accounts}).Execute(t.Context(), application.AccountCommand{Operation: "bind", ID: a.ID, Scope: "normal"}, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	c.Subscriptions = sub.Config{Version: sub.SchemaVersion,
		Sources:     []sub.Source{{ID: "audit", Kind: "account", AccountID: a.ID, Scope: "normal", Required: true}},
		Collections: []sub.Collection{{ID: "audit", Sources: []string{"audit"}}},
		Templates:   []sub.Template{{ID: "audit", Client: "uri", Version: 1, Mode: "nodes"}},
		Outputs:     []sub.Output{{ID: "audit", Collection: "audit", Template: "audit"}}}
	if _, err := s.build(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	id, err := s.Store.Queue(t.Context(), "apply", "fixture", []byte(`{}`), 1, mustJSON(t, c))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Applied(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Finish(t.Context(), id, nil); err != nil {
		t.Fatal(err)
	}
	a.Revision, a.Disabled = 1, true
	if err = s.Accounts.SaveAccount(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err = s.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("worker did not survive unavailable account", err)
	}
	var snapshot sub.Snapshot
	if _, err = ReadJSON(root+"/"+subscriptionsSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Artifacts) != 0 || len(snapshot.Diagnostics["audit"]) == 0 {
		t.Fatal("inactive subscription still published")
	}
}

func multipartRestoreRequest(t *testing.T, data []byte, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("backup", "backup.sbvpn")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/restore", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r
}

func restoreRequest(t *testing.T, data []byte, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := multipartRestoreRequest(t, data, fields)
	w := httptest.NewRecorder()
	var value backupRequest
	defer value.Close()
	if err := readBackupRequest(w, r, &value); err != nil {
		w.WriteHeader(400)
		return w
	}
	got, err := io.ReadAll(value.Backup)
	if err != nil || !bytes.Equal(got, data) || value.Password != fields["password"] {
		t.Fatal("multipart lost backup bytes")
	}
	w.WriteHeader(200)
	return w
}

func TestRestorePreviewKeepsRevisionUntilApply(t *testing.T) {
	root, core, _ := appFixture(t)
	s, config, _ := platformFixture(t, root, core)
	password := "restore-preview-password"
	data, err := s.Backup(t.Context(), []byte(password))
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.servePlatformBackup(w, multipartRestoreRequest(t, data, map[string]string{"password": password}), "admin")
	var preview struct {
		Revision int64 `json:"revision"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil || w.Code != 200 || preview.Revision != 1 {
		t.Fatal("preview lost current revision", w.Code, w.Body.String(), err)
	}
	config.App.Name = "concurrent-edit"
	id, err := s.Store.Queue(t.Context(), "apply", "test", []byte(`{}`), preview.Revision, mustJSON(t, config))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Finish(t.Context(), id, nil); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.servePlatformBackup(w, multipartRestoreRequest(t, data, map[string]string{"password": password, "apply": "true", "revision": "1"}), "admin")
	if w.Code != http.StatusConflict {
		t.Fatal("stale restore overwrote a newer configuration", w.Code, w.Body.String())
	}
}

func TestBinaryRestoreValidationAndLimitSlot(t *testing.T) {
	if w := restoreRequest(t, []byte{0, 1, 2, 255}, map[string]string{"password": "test-password"}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := restoreRequest(t, []byte{0}, map[string]string{"password": "test-password", "apply": "true"}); w.Code != 400 {
		t.Fatal("apply without revision accepted")
	}
	s := &PlatformService{}
	s.backupBusy.Store(true)
	w := httptest.NewRecorder()
	s.servePlatformBackup(w, httptest.NewRequest("POST", "/api/restore", nil), "admin")
	if w.Code != 429 {
		t.Fatal("concurrent backup admitted", w.Code)
	}
}

func TestPanelAssetConditionalCache(t *testing.T) {
	w := httptest.NewRecorder()
	servePanelAsset(w, httptest.NewRequest("GET", "/panel/app.js", nil), "panel/app.js")
	r := httptest.NewRequest("GET", "/panel/app.js", nil)
	r.Header.Set("If-None-Match", w.Header().Get("ETag"))
	w = httptest.NewRecorder()
	servePanelAsset(w, r, "panel/app.js")
	if w.Code != 304 || w.Body.Len() != 0 {
		t.Fatal(w.Code, w.Body.Len())
	}
}

func TestQueuedTaskImmediatelyInvalidatesOverview(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, _ := platformFixture(t, root, core)
	if _, err := s.overviewSnapshot(t.Context()); err != nil {
		t.Fatal(err)
	}
	id, err := s.Queue(t.Context(), PlatformAction{Operation: "service", Service: "sb-core", Value: "stop"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.overviewSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var overview struct {
		Tasks []controlstore.Task `json:"tasks"`
	}
	if err := json.Unmarshal(data, &overview); err != nil {
		t.Fatal(err)
	}
	for _, task := range overview.Tasks {
		if task.ID == id && task.State == "queued" {
			return
		}
	}
	t.Fatal("new task missing from cached overview")
}

func TestAccountDownloadsUseSharedClientCatalog(t *testing.T) {
	root, core, app := appFixture(t)
	// Plain VMess WS is common to every encoder and avoids pretending all
	// applications can import pinned certificates or Reality fingerprints.
	var inboundID string
	for _, inbound := range app.Inbounds {
		if text(inbound["type"]) == "vmess" {
			delete(inbound, "tls")
			inboundID = text(inbound["tag"])
			break
		}
	}
	if err := CommitApp(t.Context(), root, core, &app); err != nil {
		t.Fatal(err)
	}
	store, err := accounting.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := PlatformConfig{App: app}
	service := AccountService{Root: root, Core: core, Store: store, Config: &config}
	if err := store.SaveAccount(t.Context(), domain.Account{ID: "all", Name: "All"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(t.Context(), application.AccountCommand{Operation: "bind", ID: "all", Scope: "normal", InboundIDs: []string{inboundID}}, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	var clients []string
	for id := range clientprofile.All() {
		clients = append(clients, id)
	}
	token, err := store.CreateGrant(t.Context(), accounting.Grant{ID: "catalog", AccountID: "all", Scope: "normal", Clients: clients})
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range clients {
		t.Run(client, func(t *testing.T) {
			w := httptest.NewRecorder()
			service.ServeDownload(w, httptest.NewRequest("GET", "/u/"+token+"/"+client, nil))
			if w.Code != 200 || w.Body.Len() == 0 || !strings.HasPrefix(w.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("catalog format cannot be downloaded safely", w.Code, w.Body.String())
			}
		})
	}
	grant, err := store.LookupGrant(t.Context(), token)
	if err != nil || grant.Downloads != int64(len(clients)) {
		t.Fatal("new formats bypassed atomic download counting", grant.Downloads, err)
	}
}
