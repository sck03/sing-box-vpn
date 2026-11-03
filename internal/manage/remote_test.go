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
	}
	settings := remoteSettings{}
	settings.GitLab = &struct {
		Project string `json:"project"`
		Branch  string `json:"branch"`
		Token   string `json:"token"`
	}{"group/project", "main", "private-token"}
	var private atomic.Bool
	var posts atomic.Int32
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
	if err := pushGitLab(context.Background(), server.Client(), server.URL, root, settings); err == nil || posts.Load() != 0 {
		t.Fatal("public project accepted")
	}
	private.Store(true)
	if err := pushGitLab(context.Background(), server.Client(), server.URL, root, settings); err != nil || posts.Load() != 1 {
		t.Fatal("private commit failed", err, posts.Load())
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
