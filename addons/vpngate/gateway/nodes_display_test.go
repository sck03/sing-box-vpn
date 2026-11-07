package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNodeListFiltersAndOmitsProfiles(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	m.nodes = []Node{
		{Host: "jp", IP: "203.0.113.5", Country: "JP", Network: "udp", Speed: 10, Ping: 25, Profile: "private-profile"},
		{Host: "kr", IP: "203.0.113.6", Country: "KR"},
		{Host: "public-vpn-1", IP: "219.100.37.1", Country: "JP"},
	}
	handler := m.handler(context.Background())
	for _, empty := range []bool{false, true} {
		if empty {
			m.nodes = nil
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/nodes", nil))
		var nodes []map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &nodes); err != nil || nodes == nil {
			t.Fatal("node list must be an array", response.Body.String(), err)
		}
		if empty {
			if len(nodes) != 0 {
				t.Fatal("empty cache exposed nodes")
			}
			continue
		}
		if len(nodes) != 1 || len(nodes[0]) != 6 || nodes[0]["host"] != "jp" || nodes[0]["speed"] != float64(10) || nodes[0]["ping"] != float64(25) {
			t.Fatal("incorrect public node list", nodes)
		}
		if strings.Contains(response.Body.String(), "private-profile") {
			t.Fatal("public list exposed OpenVPN profile")
		}
	}
}

func BenchmarkNodeListResponse(b *testing.B) {
	cfg := defaults()
	cfg.DataDir = b.TempDir()
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	for i := 0; i < 1500; i++ {
		m.nodes = append(m.nodes, Node{Host: fmt.Sprintf("node-%d", i), IP: "203.0.113.5", Country: "JP", Network: "udp", Speed: 100000000, Ping: 25, Profile: safeTestProfile})
	}
	handler := m.handler(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/v1/nodes", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			b.Fatal(response.Code)
		}
	}
}
