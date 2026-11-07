package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failedNodeReader struct{ err error }

func (r failedNodeReader) Read([]byte) (int, error) { return 0, r.err }

type finalErrorReader struct{ data []byte }

func (r *finalErrorReader) Read(buffer []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(buffer, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

func TestNodeStreamRejectsReadFailures(t *testing.T) {
	failure := errors.New("node source interrupted")
	for _, count := range []int{0, 1, maxNodes} {
		var prefix []byte
		if count > 0 {
			prefix = benchmarkNodes(count)
		}
		nodes, err := parseNodes(io.MultiReader(bytes.NewReader(prefix), failedNodeReader{failure}))
		if !errors.Is(err, failure) || len(nodes) != 0 {
			t.Fatalf("%d-node interrupted source returned %d nodes, %v", count, len(nodes), err)
		}
	}
	if nodes, err := parseNodes(&finalErrorReader{benchmarkNodes(maxNodes)}); !errors.Is(err, io.ErrUnexpectedEOF) || len(nodes) != 0 {
		t.Fatalf("last-row read failure returned %d nodes, %v", len(nodes), err)
	}
}

func TestNodeStreamSizeAndNodeLimits(t *testing.T) {
	data := benchmarkNodes(maxNodes + 1)
	nodes, err := parseNodes(bytes.NewReader(data))
	if err != nil || len(nodes) != maxNodes {
		t.Fatalf("node count limit: %d, %v", len(nodes), err)
	}
	// Reaching the node cap must still reject an oversized response tail.
	for _, extra := range []int{0, 1} {
		padding := strings.NewReader(strings.Repeat(" ", maxNodeSource+extra-len(data)))
		nodes, err := parseNodes(io.MultiReader(bytes.NewReader(data), padding))
		if extra == 0 && (err != nil || len(nodes) != maxNodes) {
			t.Fatalf("exact-size response rejected: %d, %v", len(nodes), err)
		}
		if extra == 1 && (err == nil || len(nodes) != 0) {
			t.Fatalf("oversized response accepted: %d, %v", len(nodes), err)
		}
	}
}

func TestFailedNodeRefreshPreservesCache(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	cfg.Sources = []string{"https://nodes.example"}
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	saved := []Node{{Host: "saved-node"}}
	m.nodes = saved
	if err := saveSourceCache(cfg, "nodes.json", saved); err != nil {
		t.Fatal(err)
	}
	m.sourceClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(io.MultiReader(bytes.NewReader(benchmarkNodes(1)), failedNodeReader{io.ErrUnexpectedEOF}))}, nil
	})
	if nodes := m.refresh(context.Background(), cfg, true); len(nodes) != 1 || nodes[0].Host != saved[0].Host {
		t.Fatal("interrupted source replaced the last valid cache", nodes)
	}
	data, err := os.ReadFile(filepath.Join(cfg.DataDir, "nodes.json"))
	if err != nil || !bytes.Contains(data, []byte(saved[0].Host)) {
		t.Fatal("interrupted source changed the persisted cache", err)
	}
}

func TestManagerIgnoresInvalidNodeCaches(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	path := filepath.Join(cfg.DataDir, "nodes.json")
	if err := os.WriteFile(path, []byte("[{}]"+strings.Repeat(" ", maxNodeSource)), 0600); err != nil {
		t.Fatal(err)
	}
	m := newManager(cfg, "")
	m.sourceClient.CloseIdleConnections()
	if len(m.nodes) != 0 {
		t.Fatal("oversized disk cache loaded")
	}
	if err := saveSourceCache(cfg, "nodes.json", make([]Node, maxNodes+1)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.DataDir, "last-node.json"), []byte(`{"host":"partial-node","ip":`), 0600); err != nil {
		t.Fatal(err)
	}
	m = newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	if len(m.nodes) != 0 || m.previous.Host != "" {
		t.Fatal("invalid cache retained partially decoded nodes")
	}
}
