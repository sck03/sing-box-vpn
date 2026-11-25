package manage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestUpgradeReceiptSurvivesRestartAndCompletesOnce(t *testing.T) {
	for _, phase := range []string{"succeeded", "rolled-back", "rollback-failed"} {
		t.Run(phase, func(t *testing.T) {
			s, err := OpenPlatform(t.TempDir(), "", "", true)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err := s.Store.Initialize(t.Context(), []byte(`{}`)); err != nil {
				t.Fatal(err)
			}
			id, err := s.Store.Queue(t.Context(), "program-upgrade", "test", []byte(`{}`), 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Store.ClaimLane(t.Context(), "control"); err != nil {
				t.Fatal(err)
			}
			if err = s.awaitProgramUpgrade(t.Context(), id, "v5.1.0"); err != nil {
				t.Fatal(err)
			}
			if err = s.Store.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			task, _ := s.Store.Task(t.Context(), id)
			if task.State != "running" || task.Phase != "awaiting-panel-health" {
				t.Fatal(task)
			}
			if _, err = s.Store.Queue(t.Context(), "service", "test", []byte(`{}`), 0, nil); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Store.ClaimLane(t.Context(), "control"); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("deployment did not block control mutations", err)
			}
			// A stale or wrong-target receipt must never complete the task.
			receipt := programUpgradeReceipt{TaskID: id, TargetVersion: "v5.2.0", ActualVersion: "v5.2.0", Phase: "succeeded", Outcome: "passed"}
			if err = AtomicJSON(filepath.Join(s.Root, upgradeReceiptFile), receipt); err != nil {
				t.Fatal(err)
			}
			if err = s.reconcileProgramUpgrade(t.Context()); err != nil {
				t.Fatal(err)
			}
			task, _ = s.Store.Task(t.Context(), id)
			if task.State != "running" {
				t.Fatal("wrong-target receipt completed task")
			}
			receipt.TargetVersion, receipt.Phase = "v5.1.0", phase
			receipt.ActualVersion = "v5.0.0"
			receipt.Outcome = "failed"
			if phase == "succeeded" {
				receipt.ActualVersion, receipt.Outcome = "v5.1.0", "passed"
			}
			if err = AtomicJSON(filepath.Join(s.Root, upgradeReceiptFile), receipt); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err = s.reconcileProgramUpgrade(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			task, _ = s.Store.Task(t.Context(), id)
			want := "failed"
			if phase == "succeeded" {
				want = "succeeded"
			}
			if task.State != want || task.Phase != phase || task.Outcome != receipt.Outcome {
				t.Fatal(task)
			}
			if _, err = s.Store.ClaimLane(t.Context(), "control"); err != nil {
				t.Fatal("completed deployment kept lane blocked", err)
			}
		})
	}
}

func TestUpgradeWaitTimeoutReleasesControlLane(t *testing.T) {
	s, err := OpenPlatform(t.TempDir(), "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.Store.Queue(t.Context(), "program-upgrade", "test", []byte(`{}`), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.ClaimLane(t.Context(), "control"); err != nil {
		t.Fatal(err)
	}
	if err = s.awaitProgramUpgrade(t.Context(), id, "v5.1.0"); err != nil {
		t.Fatal(err)
	}
	task, err := s.Store.Task(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	updated := time.Unix(task.Updated, 0)
	for _, cutoff := range []time.Time{updated.Add(-upgradeWaitBudget), updated} {
		if err = s.Store.ExpireExternal(t.Context(), cutoff); err != nil {
			t.Fatal(err)
		}
		task, err = s.Store.Task(t.Context(), id)
		if err != nil || task.State != "running" {
			t.Fatal("upgrade expired before the cutoff", task, err)
		}
	}
	if err = s.Store.ExpireExternal(t.Context(), updated.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	task, err = s.Store.Task(t.Context(), id)
	if err != nil || task.State != "failed" || task.Phase != "interrupted" || task.Outcome != "failed" || task.Error == "" {
		t.Fatal("missing timeout result", task, err)
	}
	// A late success cannot overwrite the timeout or keep other work blocked.
	receipt := programUpgradeReceipt{TaskID: id, TargetVersion: "v5.1.0", ActualVersion: "v5.1.0", Phase: "succeeded", Outcome: "passed"}
	if err = AtomicJSON(filepath.Join(s.Root, upgradeReceiptFile), receipt); err != nil {
		t.Fatal(err)
	}
	if err = s.reconcileProgramUpgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	if task, err = s.Store.Task(t.Context(), id); err != nil || task.State != "failed" {
		t.Fatal("late receipt overwrote timeout", task, err)
	}
	if _, err = s.Store.Queue(t.Context(), "service", "test", []byte(`{}`), 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.ClaimLane(t.Context(), "control"); err != nil {
		t.Fatal("expired deployment kept lane blocked", err)
	}
}

func TestShareReadinessMatchesDownloadAfterUnbind(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, admin := platformFixture(t, root, core)
	if err := s.Accounts.SaveAccount(t.Context(), domain.Account{ID: "share", Name: "Share", StartMode: "first_use", DurationSeconds: 86400}); err != nil {
		t.Fatal(err)
	}
	handler := s.Handler(PanelOptions{PublicURL: "https://example.com"})
	body := mustJSON(t, accounting.Grant{ID: "share", AccountID: "share", Scope: "normal", Profiles: []string{"uri"}})
	w := httptest.NewRecorder()
	request := panelRequest("POST", "/api/grants", body, admin)
	request.AddCookie(&http.Cookie{Name: "__Host-sbvpn", Value: admin.Token})
	handler.ServeHTTP(w, request)
	if w.Code != 400 {
		t.Fatal("unbound share created", w.Code, w.Body.String())
	}
	service := s.accountService()
	if err := service.Execute(t.Context(), application.AccountCommand{Operation: "bind", ID: "share", Scope: "normal", InboundIDs: testInboundIDs(t, service)}, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	request = panelRequest("POST", "/api/grants", body, admin)
	request.AddCookie(&http.Cookie{Name: "__Host-sbvpn", Value: admin.Token})
	handler.ServeHTTP(w, request)
	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	u, err := s.Accounts.VisibleAccount(t.Context(), "share", time.Now())
	if err != nil || u.StartedAt != nil {
		t.Fatal("share activated first use", u, err)
	}
	credentials, err := s.Accounts.AccountCredentials(t.Context(), "share", "normal")
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range credentials {
		if err := service.Execute(t.Context(), application.AccountCommand{Operation: "unbind", ID: credential.ID, Generation: credential.Generation, Scope: "normal"}, new(bytes.Buffer)); err != nil {
			t.Fatal(err)
		}
	}
	for _, resource := range []string{"usage", "uri"} {
		w = httptest.NewRecorder()
		service.ServeDownload(w, httptest.NewRequest("GET", "/u/"+result.Token+"/"+resource, nil))
		want := 200
		if resource == "uri" {
			want = http.StatusForbidden
		}
		if w.Code != want || !bytes.Contains(w.Body.Bytes(), []byte("no_bound_inbound")) {
			t.Fatal(resource, w.Code, w.Body.String())
		}
	}
}

func TestInvalidPanelInitializationHasNoCommittedConfiguration(t *testing.T) {
	for _, options := range []PanelOptions{{Listen: "127.0.0.1:65536"}, {Listen: "127.0.0.1:2096", TrustedProxy: []string{"127.0.0.1/32"}}} {
		root := t.TempDir()
		s, err := OpenPlatform(root, "", "", true)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.initialize(t.Context(), "example.com", "Test", "admin", "valid-password", options); err == nil {
			t.Fatal("invalid settings initialized")
		}
		if _, err = os.Stat(filepath.Join(root, "panel.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid panel settings persisted", err)
		}
		if _, err = s.Store.State(t.Context()); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("configuration committed before validation", err)
		}
		s.Close()
	}
}

func TestPanelHealthRequiresReadyWorkerAndLocalPeer(t *testing.T) {
	s := &PlatformService{}
	handler := s.Handler(PanelOptions{PublicURL: "http://example.com"})
	for _, test := range []struct {
		peer   string
		ready  bool
		status int
	}{
		{"127.0.0.1:1234", false, 503}, {"127.0.0.1:1234", true, 200}, {"192.0.2.1:1234", true, 401},
		{"192.0.2.2:1234", true, 200},
	} {
		s.workerReady.Store(test.ready)
		request := httptest.NewRequest("GET", "http://example.com/healthz", nil)
		request.RemoteAddr = test.peer
		request = request.WithContext(context.WithValue(request.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("192.0.2.2"), Port: 2096}))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatal(test, response.Code)
		}
	}
}

func TestPanelHealthOnSpecificLocalInterface(t *testing.T) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	var localIP net.IP
	for _, address := range addresses {
		if network, ok := address.(*net.IPNet); ok && network.IP.To4() != nil && !network.IP.IsLoopback() {
			localIP = network.IP
			break
		}
	}
	if localIP == nil {
		t.Skip("no non-loopback IPv4 interface")
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort(localIP.String(), "0"))
	if err != nil {
		t.Fatal(err)
	}
	s := &PlatformService{}
	s.workerReady.Store(true)
	server := httptest.NewUnstartedServer(s.Handler(PanelOptions{PublicURL: "http://example.com"}))
	server.Listener.Close()
	server.Listener = listener
	server.Start()
	defer server.Close()
	request, err := http.NewRequest("GET", server.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "example.com"
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal("local interface health probe was denied", response.Status)
	}
}
