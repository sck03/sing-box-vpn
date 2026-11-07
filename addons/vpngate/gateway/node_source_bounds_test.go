package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func TestNodeSourceRejectsExcessStructure(t *testing.T) {
	valid := string(benchmarkNodes(1))
	for name, body := range map[string]string{
		"columns":      valid + strings.Repeat(",", 1<<18) + "\n",
		"header":       strings.Replace(valid, "\n", strings.Repeat(",extra", 64)+"\n", 1),
		"preamble":     strings.Repeat("x", 4096) + "\n" + valid,
		"record":       valid + "\"" + strings.Repeat("x\n", 1<<20) + "\"\n",
		"after limit":  string(benchmarkNodes(maxNodes)) + strings.Repeat(",", 64) + "\n",
		"unterminated": valid + "\"" + strings.Repeat("x", 2<<20),
	} {
		t.Run(name, func(t *testing.T) {
			if nodes, err := parseNodes(strings.NewReader(body)); err == nil || len(nodes) != 0 {
				t.Fatalf("accepted excessive CSV structure: %d nodes, %v", len(nodes), err)
			}
		})
	}
}

func TestNodeSourceBoundedCSVPreservesQuotedFields(t *testing.T) {
	records, err := csv.NewReader(bytes.NewReader(benchmarkNodes(1))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for len(records[0]) < 64 {
		records[0] = append(records[0], "Note")
		records[1] = append(records[1], "quoted, \"note\"\r\n下一行")
	}
	var data bytes.Buffer
	w := csv.NewWriter(&data)
	w.UseCRLF = true
	w.WriteAll(records)
	if err := w.Error(); err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(data.String(), ",\"", ",\u2003\"")
	for _, reader := range []io.Reader{strings.NewReader(body), iotest.OneByteReader(strings.NewReader(body))} {
		nodes, err := parseNodes(reader)
		if err != nil || len(nodes) != 1 || nodes[0].Profile != safeTestProfile {
			t.Fatalf("valid quoted fields rejected: %d nodes, %v", len(nodes), err)
		}
	}
}

func TestNodeSourceRecordSizeBoundary(t *testing.T) {
	valid := string(benchmarkNodes(1))
	for _, ending := range []string{"\n", "\r\n", ""} {
		for _, extra := range []int{0, 1} {
			// A quoted metadata-only row is ignored, but still subject to limits.
			row := "\"" + strings.Repeat("x", maxCSVRecord-2-len(ending)+extra) + "\"" + ending
			nodes, err := parseNodes(strings.NewReader(valid + row))
			if extra == 0 && (err != nil || len(nodes) != 1) {
				t.Fatalf("exact-size record with ending %q rejected: %d nodes, %v", ending, len(nodes), err)
			}
			if extra == 1 && (err == nil || len(nodes) != 0) {
				t.Fatalf("oversize record with ending %q accepted: %d nodes, %v", ending, len(nodes), err)
			}
		}
	}
}

func TestNodeSourceRejectsExcessColumnsBeforeReadingBody(t *testing.T) {
	body := bytes.NewReader(append(benchmarkNodes(1), strings.Repeat(",", 1<<18)...))
	if _, err := parseNodes(body); err == nil {
		t.Fatal("accepted excess columns")
	}
	if body.Size()-int64(body.Len()) > 8192 {
		t.Fatal("read the oversized row before rejecting it")
	}
}

func TestExcessCSVRefreshPreservesCache(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	saved := []Node{{Host: "saved-node"}}
	m.nodes = saved
	if err := saveSourceCache(cfg, "nodes.json", saved); err != nil {
		t.Fatal(err)
	}
	body := string(benchmarkNodes(1)) + strings.Repeat(",", 64) + "\n"
	m.sourceClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testHTTPResponse(body), nil
	})
	if got := m.refresh(context.Background(), cfg, true); !reflect.DeepEqual(got, saved) {
		t.Fatal("excessive source replaced in-memory cache")
	}
	if got := loadSourceCache[[]Node](cfg, "nodes.json"); !reflect.DeepEqual(got, saved) {
		t.Fatal("excessive source replaced disk cache")
	}
}

func BenchmarkNodeSourceExcessColumns(b *testing.B) {
	body := string(benchmarkNodes(1)) + strings.Repeat(",", 1<<18) + "\n"
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parseNodes(strings.NewReader(body)); err == nil {
			b.Fatal("accepted excess columns")
		}
	}
}
