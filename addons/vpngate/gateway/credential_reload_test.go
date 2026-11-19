package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/manage"
)

func TestManagedCredentialReload(t *testing.T) {
	if os.Getenv("MANAGED_CORE_BINARY") == "" {
		t.Skip("set MANAGED_CORE_BINARY")
	}
	binary := os.Getenv("SING_BOX_BINARY")
	if binary == "" {
		t.Fatal("SING_BOX_BINARY required")
	}
	root, used := t.TempDir(), map[int]bool{}
	p, err := manage.NewProfile(root, used, "")
	if err != nil {
		t.Fatal(err)
	}
	entries := manage.MakeInbounds(p, "")
	delete(entries[0], "tls")
	delete(entries[0], "_managed_flow")
	app := manage.App{Name: "reload", Address: "127.0.0.1", Nodes: manage.MakeNodes(p, ""), SocksPort: 40000}
	publishProtocolFixture(t, root, binary, &app)
	var exported struct {
		Outbounds []manage.Object `json:"outbounds"`
	}
	if _, err = manage.ReadJSON(filepath.Join(root, "sbox.json"), &exported); err != nil {
		t.Fatal(err)
	}
	server, err := manage.ServerConfig(entries, nil, nil, 40000)
	if err != nil {
		t.Fatal(err)
	}
	store := startManagedCore(t, root, "reload", "normal", server, entries)
	control, err := controlstore.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	runtimes, err := control.Runtimes(t.Context())
	readyDeadline := time.Now().Add(20 * time.Second)
	for err == nil && len(runtimes) == 0 && time.Now().Before(readyDeadline) {
		time.Sleep(50 * time.Millisecond)
		runtimes, err = control.Runtimes(t.Context())
	}
	if err != nil || len(runtimes) != 1 {
		t.Fatal(runtimes, err)
	}
	started := runtimes[0]["started"]
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, e := echo.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	connections := map[string]net.Conn{}
	nodes := map[string]manage.Object{}
	client := func(kind, suffix string, node manage.Object) net.Conn {
		port, e := manage.AvailablePort(used)
		if e != nil {
			t.Fatal(e)
		}
		config := manage.Object{"inbounds": []manage.Object{{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": []manage.Object{node}, "route": manage.Object{"final": node["tag"]}}
		protocolCore(t, root, kind+suffix, binary, config, port)
		c, _ := protocolMixed(t, port, 1, echo.Addr().String())
		t.Cleanup(func() { c.Close() })
		if e = echoPayload(c, kind+suffix); e != nil {
			t.Fatal(e)
		}
		return c
	}
	for _, entry := range entries {
		kind := entry["type"].(string)
		data, _ := json.Marshal(protocolEntry(exported.Outbounds, kind))
		var node manage.Object
		_ = json.Unmarshal(data, &node)
		if kind == "vless" {
			delete(node, "tls")
			delete(node, "flow")
		}
		nodes[kind] = node
		connections[kind] = client(kind, "-old", node)
	}
	for _, entry := range entries {
		kind := entry["type"].(string)
		t.Run(kind, func(t *testing.T) {
			id := "normal-alice-" + kind
			if err := store.Rotate(t.Context(), id, "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff", "new-independent-password", 1); err != nil {
				t.Fatal(err)
			}
			v, err := store.CredentialPublication(t.Context(), "normal")
			if err != nil {
				t.Fatal(err)
			}
			if err = store.CredentialsPublished(t.Context(), "normal", v.Revision); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(20 * time.Second)
			for {
				current, e := store.CredentialPublication(t.Context(), "normal")
				if e != nil {
					t.Fatal(e)
				}
				if current.Loaded >= v.Revision {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("reload acknowledgement timeout: %+v", current)
				}
				time.Sleep(50 * time.Millisecond)
			}
			if err = echoPayload(connections[kind], "old-generation"); err == nil {
				t.Fatal("old credential still forwards")
			}
			for other, c := range connections {
				if other != kind {
					if err = echoPayload(c, "uninterrupted-"+kind); err != nil {
						t.Fatalf("%s disrupted by %s: %v", other, kind, err)
					}
				}
			}
			node := nodes[kind]
			if kind == "vless" || kind == "vmess" || kind == "tuic" {
				node["uuid"] = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
			}
			if kind == "tuic" || kind == "hysteria2" || kind == "anytls" {
				node["password"] = "new-independent-password"
			}
			connections[kind] = client(kind, "-new", node)
			now, e := control.Runtimes(t.Context())
			if e != nil || len(now) != 1 || fmt.Sprint(now[0]["started"]) != fmt.Sprint(started) {
				t.Fatal("core process restarted", now, e)
			}
		})
	}
}
