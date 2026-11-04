package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"testing"
)

func benchmarkNodes(count int) []byte {
	var data bytes.Buffer
	w := csv.NewWriter(&data)
	_ = w.Write([]string{"HostName", "IP", "CountryShort", "Ping", "Speed", "OpenVPN_ConfigData_Base64"})
	profile := base64.StdEncoding.EncodeToString([]byte(safeTestProfile))
	for i := 0; i < count; i++ {
		_ = w.Write([]string{fmt.Sprintf("node-%d", i), "203.0.113.5", "JP", "10", "1000000", profile})
	}
	w.Flush()
	return data.Bytes()
}

func BenchmarkParseNodes(b *testing.B) {
	data := benchmarkNodes(250)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodes, err := parseNodes(data)
		if err != nil || len(nodes) != 250 {
			b.Fatalf("parse: %d nodes, %v", len(nodes), err)
		}
	}
}

func BenchmarkUDPFrameRead(b *testing.B) {
	frame := make([]byte, 1402)
	binary.BigEndian.PutUint16(frame[:2], 1400)
	r := bytes.NewReader(frame)
	frames := frameReader{source: r, limit: maxPacket}
	if _, err := frames.read(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(1400)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Reset(frame)
		if _, err := frames.read(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPinnedCandidates(b *testing.B) {
	nodes, err := parseNodes(benchmarkNodes(250))
	if err != nil {
		b.Fatal(err)
	}
	cfg := defaults()
	cfg.Mode, cfg.FixedHost = "fixed", nodes[0].Host
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if pool := candidates(cfg, nodes, nodes[0], false); len(pool) != 1 {
			b.Fatal("pinned node was not selected")
		}
	}
}
