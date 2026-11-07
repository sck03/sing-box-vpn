package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPersistentNodeCachesBelongToSources(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	node := Node{Host: "saved", IP: "203.0.113.5", Country: "JP", Profile: safeTestProfile}
	if err := saveSourceCache(cfg, "nodes.json", []Node{node}); err != nil {
		t.Fatal(err)
	}
	if err := saveSourceCache(cfg, "last-node.json", node); err != nil {
		t.Fatal(err)
	}
	for _, matching := range []bool{true, false} {
		current := cfg
		if !matching {
			current.Sources = []string{"https://new.example/nodes"}
		}
		m := newManager(current, "")
		m.sourceClient.CloseIdleConnections()
		if (len(m.nodes) == 1 && m.nodes[0].Host == node.Host && m.previous.Host == node.Host) != matching {
			t.Fatal("cache source ownership was not respected", matching)
		}
		if !matching && (len(m.nodes) != 0 || m.previous.Host != "") {
			t.Fatal("restart loaded removed-source candidates")
		}
	}
	// Unlabelled files from development builds are discarded, not migrated.
	if err := atomicJSON(filepath.Join(cfg.DataDir, "nodes.json"), []Node{node}); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(filepath.Join(cfg.DataDir, "last-node.json"), node); err != nil {
		t.Fatal(err)
	}
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	if len(m.nodes) != 0 || m.previous.Host != "" {
		t.Fatal("unlabelled development cache was accepted")
	}
}

func TestSourceChangedBackRejectsInFlightResponse(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			cfg := defaults()
			cfg.DataDir, cfg.RunDir = t.TempDir(), t.TempDir()
			cfg.ProxyUser, cfg.ProxyPass = "test", strings.Repeat("a", 32)
			m := newManager(cfg, filepath.Join(cfg.DataDir, "gateway.json"))
			defer m.sourceClient.CloseIdleConnections()
			started, release := make(chan struct{}), make(chan struct{})
			m.sourceClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				close(started)
				<-release
				if !success {
					return nil, errors.New("old request failed")
				}
				return testHTTPResponse(string(benchmarkNodes(1))), nil
			})
			result := make(chan []Node, 1)
			finished := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			go func() { defer close(finished); result <- m.refresh(context.Background(), cfg, true) }()
			defer func() { unblock(); <-finished }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("refresh did not start")
			}
			other := cfg
			other.Sources = []string{"https://other.example/nodes"}
			for _, next := range []Config{other, cfg} {
				if err := m.apply(next, false); err != nil {
					t.Fatal(err)
				}
			}
			// Preserve a newer cache even if an old request returns after A -> B -> A.
			newer := []Node{{Host: "newer"}}
			m.mu.Lock()
			m.nodes = newer
			m.mu.Unlock()
			if err := saveSourceCache(cfg, "nodes.json", newer); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(cfg.DataDir, "nodes.json"))
			if err != nil {
				t.Fatal(err)
			}
			unblock()
			select {
			case nodes := <-result:
				if len(nodes) != 0 {
					t.Fatal("obsolete request returned candidates")
				}
			case <-time.After(time.Second):
				t.Fatal("refresh did not finish")
			}
			after, err := os.ReadFile(filepath.Join(cfg.DataDir, "nodes.json"))
			if err != nil || !bytes.Equal(before, after) || m.nodes[0].Host != "newer" {
				t.Fatal("obsolete request replaced the current cache", err)
			}
		})
	}
}

func TestSourceChangeDropsPreviousCandidates(t *testing.T) {
	cfg := defaults()
	cfg.DataDir, cfg.RunDir = t.TempDir(), t.TempDir()
	cfg.ProxyUser, cfg.ProxyPass = "test", strings.Repeat("a", 32)
	m := newManager(cfg, filepath.Join(cfg.DataDir, "gateway.json"))
	defer m.sourceClient.CloseIdleConnections()
	node := Node{Host: "old-source", IP: "203.0.113.5", Country: "JP", Profile: safeTestProfile}
	m.nodes, m.previous, m.nodesUpdated = []Node{node}, node, time.Now()
	m.failed["new-source-node"] = time.Now().Add(time.Minute)
	cfg.Sources = []string{"https://new.example/nodes"}
	if err := m.apply(cfg, false); err != nil {
		t.Fatal(err)
	}
	m.sourceClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("new source unavailable")
	})
	if nodes := m.reconnectCandidates(cfg, m.refresh(context.Background(), cfg, false), false); len(nodes) != 0 {
		t.Fatal("new source failure reused removed-source candidates")
	}
	if len(m.failed) != 0 {
		t.Fatal("old source cooldown blocked new source nodes")
	}
}

func TestSourceChangeCancelsBackgroundFetch(t *testing.T) {
	cfg := defaults()
	cfg.DataDir, cfg.RunDir = t.TempDir(), t.TempDir()
	cfg.ProxyUser, cfg.ProxyPass = "test", strings.Repeat("a", 32)
	m := newManager(cfg, filepath.Join(cfg.DataDir, "gateway.json"))
	defer m.sourceClient.CloseIdleConnections()
	defer m.stopRefresh()
	started, cancelled := make(chan struct{}), make(chan struct{})
	m.sourceClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
		return nil, r.Context().Err()
	})
	m.scheduleRefresh(context.Background(), cfg, true)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	cfg.Sources = []string{"https://new.example/nodes"}
	if err := m.apply(cfg, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("removed source retained the refresh slot")
	}
}

func TestRefreshRejectsObsoleteConfiguration(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	m.nodes, m.nodesUpdated = []Node{{Host: "current-source"}}, time.Now()
	obsolete := cfg
	obsolete.Sources = []string{"https://removed.example/nodes"}
	if nodes := m.refresh(context.Background(), obsolete, false); len(nodes) != 0 {
		t.Fatal("obsolete caller received current source cache")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if nodes := m.refresh(ctx, cfg, false); len(nodes) != 0 {
		t.Fatal("cancelled caller received cached candidates")
	}
}
