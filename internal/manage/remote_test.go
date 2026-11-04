package manage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGitLabRefusesPublicProjectsAndCommitsFilesTogether(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"sbox.json", "clmi.yaml", "jhsub.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("test-content"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := AtomicBytes(filepath.Join(root, "vpngate", name), []byte("vpn-content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	settings := remoteSettings{}
	settings.GitLab = &struct {
		Project string `json:"project"`
		Branch  string `json:"branch"`
		Token   string `json:"token"`
	}{"group/project", "main", "private-token"}
	var private atomic.Bool
	var posts atomic.Int32
	var vpn atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "private-token" {
			t.Error("missing private authorization")
		}
		if strings.HasSuffix(r.URL.Path, "/repository/commits") {
			posts.Add(1)
			var body Object
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(objects(body["actions"])) != 3 {
				t.Error("subscriptions were not committed together")
			}
			for _, action := range objects(body["actions"]) {
				if strings.HasPrefix(text(action["file_path"]), "vpngate/") != vpn.Load() {
					t.Error("GitLab scopes overwrite each other")
				}
				if vpn.Load() && action["content"] != "vpn-content" {
					t.Error("VPNGate push read ordinary files")
				}
			}
			io.WriteString(w, `{}`)
			return
		}
		if strings.Contains(r.URL.Path, "/repository/files/") {
			w.WriteHeader(404)
			return
		}
		visibility := "public"
		if private.Load() {
			visibility = "private"
		}
		json.NewEncoder(w).Encode(Object{"visibility": visibility})
	}))
	defer server.Close()
	if err := pushGitLab(context.Background(), server.Client(), server.URL, root, normalScope, settings); err == nil || posts.Load() != 0 {
		t.Fatal("public project accepted")
	}
	private.Store(true)
	if err := pushGitLab(context.Background(), server.Client(), server.URL, root, normalScope, settings); err != nil || posts.Load() != 1 {
		t.Fatal("private commit failed", err, posts.Load())
	}
	vpn.Store(true)
	if err := pushGitLab(context.Background(), server.Client(), server.URL, root, vpngateScope, settings); err != nil || posts.Load() != 2 {
		t.Fatal("private VPNGate commit failed", err, posts.Load())
	}
}
func TestRemoteFailureDoesNotExposeToken(t *testing.T) {
	client := &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("request failed at /botSECRET/sendMessage")
	})}
	err := remoteRequest(context.Background(), client, "https://example.test/botSECRET/sendMessage", "POST", Object{}, nil, nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("remote error exposed token", err)
	}
}

func TestRemoteRedirectCannotReportPushSuccess(t *testing.T) {
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	err := remoteRequest(context.Background(), client, server.URL, http.MethodPost, Object{"content": "subscription"}, map[string]string{"PRIVATE-TOKEN": "secret"}, nil)
	var status httpStatusError
	if !errors.As(err, &status) || status != http.StatusTemporaryRedirect || redirected.Load() != 0 {
		t.Fatal("redirected push reported success or forwarded credentials", err, redirected.Load())
	}
}
