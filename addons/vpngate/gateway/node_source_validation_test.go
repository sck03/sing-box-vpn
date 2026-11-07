package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestNodeSourceRejectsMalformedCSV(t *testing.T) {
	for _, count := range []int{1, maxNodes} {
		for _, tail := range []string{"\"unfinished\n", "bad\"quote,field\n", "\"closed\"garbage,field\n"} {
			input := append(benchmarkNodes(count), tail...)
			if nodes, err := parseNodes(bytes.NewReader(input)); err == nil || len(nodes) != 0 {
				t.Fatalf("accepted malformed CSV after %d nodes: %q (%d nodes, %v)", count, tail, len(nodes), err)
			}
		}
	}
}

func TestMalformedCSVRefreshPreservesCache(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	saved := []Node{{Host: "saved-node"}}
	m.nodes = saved
	if err := saveSourceCache(cfg, "nodes.json", saved); err != nil {
		t.Fatal(err)
	}
	body := string(benchmarkNodes(1)) + "\"unfinished\n"
	m.sourceClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testHTTPResponse(body), nil
	})
	if got := m.refresh(context.Background(), cfg, true); !reflect.DeepEqual(got, saved) {
		t.Fatal("malformed source replaced in-memory cache")
	}
	if got := loadSourceCache[[]Node](cfg, "nodes.json"); !reflect.DeepEqual(got, saved) {
		t.Fatal("malformed source replaced disk cache")
	}
}

func TestNodeSourceProfilesRemainIndependent(t *testing.T) {
	var data bytes.Buffer
	w := csv.NewWriter(&data)
	w.Write([]string{"HostName", "IP", "CountryShort", "OpenVPN_ConfigData_Base64", "Note"})
	profiles := []string{
		strings.Replace(safeTestProfile, "test-certificate", strings.Repeat("A", 8192), 1),
		safeTestProfile,
		strings.Replace(safeTestProfile, "test-certificate", strings.Repeat("B", 16384), 1),
	}
	for i, profile := range profiles {
		w.Write([]string{strings.Repeat("n", i+1), "203.0.113.5", "JP", base64.StdEncoding.EncodeToString([]byte(profile)), "quoted, \"note\"\nnext line"})
	}
	w.Flush()
	io.WriteString(&data, "*\n") // VPNGate's unquoted terminator remains valid CSV.
	nodes, err := parseNodes(&data)
	if err != nil || len(nodes) != len(profiles) {
		t.Fatalf("parse: %d nodes, %v", len(nodes), err)
	}
	for i, node := range nodes {
		if node.Profile != profiles[i] {
			t.Fatalf("profile %d changed after parsing subsequent records", i)
		}
	}
}
