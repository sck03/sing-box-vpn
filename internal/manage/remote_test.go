package manage

import (
	"context"
	"encoding/json"
	"errors"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGitLabDeliveryIsPrivateAndUsesSelectedArtifact(t *testing.T) {
	var private atomic.Bool
	var posts atomic.Int32
	var exists atomic.Bool
	delivery := sub.Delivery{Kind: "gitlab", Token: "private-token", Filename: "normal/output.yaml", Username: "main"}
	artifact := sub.Artifact{Data: []byte("selected-content")}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "private-token" {
			t.Error("missing authorization")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/repository/commits"):
			posts.Add(1)
			var body struct {
				Actions []struct {
					Action  string `json:"action"`
					Path    string `json:"file_path"`
					Content string `json:"content"`
				} `json:"actions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			want := "create"
			if exists.Load() {
				want = "update"
			}
			if len(body.Actions) != 1 || body.Actions[0].Action != want || body.Actions[0].Path != delivery.Filename || body.Actions[0].Content != string(artifact.Data) {
				t.Error("wrong artifact/action", body)
			}
			io.WriteString(w, `{}`)
		case strings.Contains(r.URL.Path, "/repository/files/"):
			if r.Method != "HEAD" {
				t.Error("downloaded remote file")
			}
			if !exists.Load() {
				w.WriteHeader(404)
			} else {
				w.Header().Set("Content-Length", "8000000")
			}
		default:
			visibility := "public"
			if private.Load() {
				visibility = "private"
			}
			json.NewEncoder(w).Encode(Object{"visibility": visibility})
		}
	}))
	defer server.Close()
	delivery.URL = server.URL
	if err := sendSubscriptionArtifact(t.Context(), server.Client(), delivery, artifact); err == nil || posts.Load() != 0 {
		t.Fatal("public project accepted")
	}
	private.Store(true)
	for _, update := range []bool{false, true} {
		exists.Store(update)
		if err := sendSubscriptionArtifact(t.Context(), server.Client(), delivery, artifact); err != nil {
			t.Fatal(err)
		}
	}
	if posts.Load() != 2 {
		t.Fatal(posts.Load())
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
