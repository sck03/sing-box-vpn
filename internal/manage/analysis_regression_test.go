package manage

import (
	"context"

	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestDefaultGrantRejectsPrefixImpersonation(t *testing.T) {
	c := fixtureSubscriptionConfig()
	c.Outputs = append(c.Outputs, sub.Output{ID: "normal-private", Collection: "normal", Template: "normal-sing-box"})
	root, token := t.TempDir(), strings.Repeat("a", 64)
	subscriptionFixture(t, root, token, "body")
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), c); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	subscriptionHandler(root).ServeHTTP(response, httptest.NewRequest("GET", "/s/"+token+"/normal-private", nil))
	if response.Code != 404 {
		t.Fatal("custom output implicitly granted", response.Code)
	}
}

func TestRefresh304MergesMetadataAtomically(t *testing.T) {
	header := "download=17; expire=2000000000"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `"v1"` {
			t.Error("missing validator")
		}
		w.Header().Set("subscription-userinfo", header)
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	source := sub.Source{ID: "remote", Kind: "remote", Format: "uri", URL: server.URL}
	upload, download := uint64(7), uint64(9)
	now := time.Now()
	old := sub.SourceCache{SourceHash: sub.SourceHash(source), ETag: `"v1"`, Nodes: []sub.Node{{Name: "cached"}}, Success: now.Add(-time.Hour), Usage: sub.Usage{Upload: &upload, Download: &download}}
	got := refreshSource(context.Background(), server.Client(), source, old, now)
	if got.Error != "" || *got.Usage.Upload != 7 || *got.Usage.Download != 17 || *got.Usage.Expire != 2000000000 || !got.Success.Equal(now) {
		t.Fatalf("metadata not merged: %+v", got)
	}
	header = "download=bad; expire=2000000001"
	bad := refreshSource(context.Background(), server.Client(), source, got, now.Add(time.Minute))
	if bad.Error == "" || *bad.Usage.Expire != *got.Usage.Expire || !bad.Success.Equal(got.Success) {
		t.Fatal("invalid metadata partially committed")
	}
	header = ""
	kept := refreshSource(context.Background(), server.Client(), source, got, now.Add(time.Minute))
	if kept.Usage.Expire == nil || *kept.Usage.Download != 17 {
		t.Fatal("missing 304 headers erased metadata")
	}
}

func TestLocalMutationSurvivesUnrelatedRequiredRemoteFailure(t *testing.T) {
	root, core, app := appFixture(t)
	c, r, _, err := loadSubscriptions(root)
	if err != nil {
		t.Fatal(err)
	}
	c.Sources = append(c.Sources, sub.Source{ID: "remote", Kind: "remote", Format: "uri", URL: "https://invalid.example/sub", Required: true})
	c.Collections = append(c.Collections, sub.Collection{ID: "remote", Sources: []string{"remote"}})
	c.Outputs = append(c.Outputs, sub.Output{ID: "remote", Collection: "remote", Template: "normal-uri"})
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), c); err != nil {
		t.Fatal(err)
	}
	app.Address = "new.example.com"
	if err := publishFixtureApp(context.Background(), root, core, &app); err != nil {
		t.Fatal(err)
	}
	var snapshot sub.Snapshot
	if _, err := ReadJSON(filepath.Join(root, subscriptionsSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(snapshot.Artifacts["normal-uri"].Data), "new.example.com") || len(snapshot.Artifacts["remote"].Data) != 0 {
		t.Fatal("local publication or unavailable output invalid")
	}
	_ = r
}

func TestScheduledRefreshPrunesExpiredOptionalWithoutFetch(t *testing.T) {
	root, core, _ := appFixture(t)
	c, r, local, err := loadSubscriptions(root)
	if err != nil {
		t.Fatal(err)
	}
	source := sub.Source{ID: "remote", Kind: "remote", Format: "nodes", URL: "https://invalid.example/sub", MaxStaleSeconds: 60}
	c.Sources = append(c.Sources, source)
	c.Collections[0].Sources = append(c.Collections[0].Sources, "remote")
	node := local[normalScope][0]
	node.ID, node.Name, node.Server = "remote-node", "remote-node", "upstream.example"
	r.Sources[source.ID] = sub.SourceCache{SourceHash: sub.SourceHash(source), Nodes: []sub.Node{node}, Success: time.Now(), Next: time.Now().Add(time.Hour)}
	files, _, _, err := compileSubscriptions(context.Background(), root, core, c, r, local, sub.Strict)
	if err != nil {
		t.Fatal(err)
	}
	if err := publish(root, files); err != nil {
		t.Fatal(err)
	}
	cached := r.Sources[source.ID]
	cached.Success = time.Now().Add(-time.Hour)
	r.Sources[source.ID] = cached
	if err := AtomicJSON(filepath.Join(root, subscriptionsRuntime), r); err != nil {
		t.Fatal(err)
	}
	if err := refreshSubscriptions(context.Background(), root, core, false); err != nil {
		t.Fatal(err)
	}
	data := readArtifact(t, root, "normal-uri")
	if strings.Contains(string(data), "upstream.example") || len(data) == 0 {
		t.Fatal("expired optional source not pruned")
	}
}
