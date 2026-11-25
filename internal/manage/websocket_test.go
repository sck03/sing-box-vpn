package manage

import (
	"context"
	"path/filepath"

	"strings"
	"testing"
)

func TestWebSocketRejectsUnusablePaths(t *testing.T) {
	for _, path := range []string{"", "ws", "/ws?ed=2048", "/ws#fragment", "/ws%2Fchild", "/ws child", "/ws\\child", "/" + strings.Repeat("x", 255)} {
		node := ManagedNode{ID: "ws", Protocol: "vmess", Port: 443, Transport: ManagedTransport{Type: "ws", Path: path}}
		if err := node.Validate(); err == nil {
			t.Fatal("accepted invalid path", path)
		}
	}
	for _, path := range []string{"/", "/ws-._~", "/普通/日本🚀", "/" + strings.Repeat("x", 254)} {
		node := ManagedNode{ID: "ws", Protocol: "vmess", Port: 443, Transport: ManagedTransport{Type: "ws", Path: path}}
		if err := node.Validate(); err != nil {
			t.Fatal(path, err)
		}
	}
}

func TestWebSocketRejectsInvalidStoredPathBeforePublish(t *testing.T) {
	root, core, app := appFixture(t)
	before := readBytes(t, filepath.Join(root, "app.json"))
	for i := range app.Nodes {
		if app.Nodes[i].Protocol == "vmess" {
			app.Nodes[i].Transport.Path = "/ws?ed=2048"
		}
	}
	if err := publishFixtureApp(context.Background(), root, core, &app); err == nil {
		t.Fatal("published an unusable WebSocket subscription")
	}
	if string(before) != string(readBytes(t, filepath.Join(root, "app.json"))) {
		t.Fatal("invalid WebSocket configuration was published")
	}
}
