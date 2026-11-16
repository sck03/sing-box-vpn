package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"github.com/sck03/sing-box-vpn/internal/manage"
)

func startManagedCore(t *testing.T, root, name, instance string, config manage.Object, entries []manage.Object) *accounting.Store {
	t.Helper()
	binary := os.Getenv("MANAGED_CORE_BINARY")
	if binary == "" {
		t.Skip("set MANAGED_CORE_BINARY for metered runtime acceptance")
	}
	store, err := accounting.Open(filepath.Join(root, "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	for _, id := range []string{"alice", "bob"} {
		if _, err = store.Account(t.Context(), id); err != nil {
			if err = store.SaveAccount(t.Context(), domain.Account{ID: id, Name: id}); err != nil {
				t.Fatal(err)
			}
		}
		for _, entry := range entries {
			users := entry["users"].([]manage.Object)
			kind := entry["type"].(string)
			c := domain.Credential{ID: instance + "-" + id + "-" + kind, AccountID: id, RuntimeID: instance, InboundID: entry["tag"].(string), Protocol: kind, Generation: 1}
			c.UUID, _ = users[0]["uuid"].(string)
			c.Password, _ = users[0]["password"].(string)
			if id == "bob" {
				c.UUID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
				c.Password = "bob-independent-password"
			}
			if err = store.Bind(t.Context(), c); err != nil {
				t.Fatal(err)
			}
		}
	}
	path := filepath.Join(root, name+"-managed.json")
	if err = manage.AtomicJSON(path, config); err != nil {
		t.Fatal(err)
	}
	protocolProcess(t, root, name+"-managed", exec.Command(binary, "--config", path, "--database", filepath.Join(root, "accounts.db"), "--instance", instance), entries[0]["listen_port"].(int))
	return store
}

func TestManagedAccountIsolation(t *testing.T) {
	if os.Getenv("MANAGED_CORE_BINARY") == "" {
		t.Skip("set MANAGED_CORE_BINARY")
	}
	binary := os.Getenv("SING_BOX_BINARY")
	if binary == "" {
		t.Fatal("SING_BOX_BINARY required")
	}
	root := t.TempDir()
	used := map[int]bool{}
	p, err := manage.NewProfile(root, used, "")
	if err != nil {
		t.Fatal(err)
	}
	// VLESS without Reality here isolates account enforcement from the Reality
	// handshake, which the existing real-protocol matrix exercises separately.
	entries := manage.MakeInbounds(p, "")
	delete(entries[0], "tls")
	entries[0]["users"].([]manage.Object)[0]["flow"] = ""
	app := manage.App{Name: "accounts", Address: "127.0.0.1", Profile: p, Inbounds: manage.MakeInbounds(p, ""), SocksPort: 40000}
	if err = manage.CommitApp(t.Context(), root, binary, &app); err != nil {
		t.Fatal(err)
	}
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
	store := startManagedCore(t, root, "isolation", "normal", server, entries)
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { echo.Close() })
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	for i, entry := range entries {
		t.Run(entry["type"].(string), func(t *testing.T) {
			a, _ := store.Account(t.Context(), "alice")
			a.Disabled = false
			a.QuotaBytes = nil
			if err = store.SaveAccount(t.Context(), a.Account); err != nil {
				t.Fatal(err)
			}
			clients := map[string]int{}
			connections := map[string]net.Conn{}
			for _, id := range []string{"alice", "bob"} {
				data, _ := json.Marshal(exported.Outbounds[i])
				var node manage.Object
				_ = json.Unmarshal(data, &node)
				kind := entry["type"].(string)
				if kind == "vless" {
					delete(node, "tls")
					delete(node, "flow")
				}
				if id == "bob" {
					if kind == "vless" || kind == "vmess" || kind == "tuic" {
						node["uuid"] = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
					}
					if kind == "hysteria2" || kind == "tuic" || kind == "anytls" {
						node["password"] = "bob-independent-password"
					}
				}
				port, err := manage.AvailablePort(used)
				if err != nil {
					t.Fatal(err)
				}
				clients[id] = port
				client := manage.Object{"inbounds": []manage.Object{{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": []manage.Object{node}, "route": manage.Object{"final": node["tag"]}}
				protocolCore(t, root, kind+"-"+id, binary, client, port)
				c, _ := protocolMixed(t, port, 1, echo.Addr().String())
				connections[id] = c
				if err := echoPayload(c, id); err != nil {
					t.Fatal(id, err)
				}
			}
			a, _ = store.Account(t.Context(), "alice")
			a.Disabled = true
			if err = store.SaveAccount(t.Context(), a.Account); err != nil {
				t.Fatal(err)
			}
			time.Sleep(350 * time.Millisecond)
			if err = echoPayload(connections["alice"], "blocked"); err == nil {
				t.Fatal("disabled account kept forwarding")
			}
			if err = echoPayload(connections["bob"], "still-active"); err != nil {
				t.Fatal("unrelated account interrupted", err)
			}
			// The same client core (and therefore cached QUIC/AnyTLS session) must
			// not establish a new usable stream after disabling its account.
			c, dialErr := accountProxy(clients["alice"], echo.Addr().String())
			if dialErr == nil {
				if err = echoPayload(c, "new-stream"); err == nil {
					t.Fatal("old authenticated session bypassed disable")
				}
				c.Close()
			}
			for _, c := range connections {
				c.Close()
			}
		})
	}
	a, err := store.Account(t.Context(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Account(t.Context(), "bob")
	if err != nil {
		t.Fatal(err)
	}
	if a.Upload == 0 || a.Download == 0 || b.Upload == 0 || b.Download == 0 {
		t.Fatal("missing independent counters", a, b)
	}
}

func echoPayload(c net.Conn, payload string) error {
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(c, payload); err != nil {
		return err
	}
	data := make([]byte, len(payload))
	if _, err := io.ReadFull(c, data); err != nil {
		return err
	}
	if string(data) != payload {
		return errors.New("echo mismatch")
	}
	return nil
}

func accountProxy(port int, target string) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err = c.Write([]byte{5, 1, 0}); err != nil {
		c.Close()
		return nil, err
	}
	var greeting [2]byte
	if _, err = io.ReadFull(c, greeting[:]); err != nil {
		c.Close()
		return nil, err
	}
	if _, err = c.Write(requestAddress(1, target)); err != nil {
		c.Close()
		return nil, err
	}
	var reply [4]byte
	if _, err = io.ReadFull(c, reply[:]); err != nil {
		c.Close()
		return nil, err
	}
	if reply[1] != 0 {
		c.Close()
		return nil, errors.New("proxy rejected stream")
	}
	length := 6
	if reply[3] == 4 {
		length = 18
	} else if reply[3] != 1 {
		c.Close()
		return nil, errors.New("unexpected proxy address")
	}
	if _, err = io.ReadFull(c, make([]byte, length)); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
