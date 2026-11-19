package manage

import (
	"path/filepath"
	"testing"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestSubscriptionScheduleReadsOnlyDeadlines(t *testing.T) {
	root := t.TempDir()
	next := time.Now().Add(time.Hour).UTC()
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), sub.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	// Irrelevant material is deliberately incompatible with Node/RawMessage
	// storage types; scheduling still consumes only valid deadline metadata.
	runtime := Object{"sources": Object{"remote": Object{"next": next, "nodes": "unused"}}, "resources": Object{"template:t": Object{"next": next, "body": "unused"}}}
	if err := AtomicJSON(filepath.Join(root, subscriptionsRuntime), runtime); err != nil {
		t.Fatal(err)
	}
	if err := AtomicBytes(filepath.Join(root, subscriptionsLocal), []byte("unused"), 0600); err != nil {
		t.Fatal(err)
	}
	_, schedule, err := loadSubscriptionSchedule(root)
	if err != nil || !schedule.Sources["remote"].Next.Equal(next) || !schedule.Resources["template:t"].Next.Equal(next) {
		t.Fatal(schedule, err)
	}
}

func BenchmarkSubscriptionScheduleRead(b *testing.B) {
	root := b.TempDir()
	config := sub.DefaultConfig()
	config.Sources = []sub.Source{{ID: "remote", Kind: "remote", Format: "auto", URL: "https://example.com/sub", Required: true}}
	nodes := make([]sub.Node, 4096)
	for i := range nodes {
		nodes[i] = sub.Node{ID: "node", Name: "Benchmark", Protocol: "vless", Server: "example.com", Port: 443, UUID: "f5dbdb8a-84c0-4c3a-b511-f5f599b37651"}
	}
	for path, data := range map[string]any{
		subscriptionsConfig:  config,
		subscriptionsRuntime: sub.Runtime{Sources: map[string]sub.SourceCache{"remote": {Nodes: nodes, Next: time.Now().Add(time.Hour)}}},
		subscriptionsLocal:   map[string][]sub.Node{"local": nodes},
	} {
		if err := AtomicJSON(filepath.Join(root, path), data); err != nil {
			b.Fatal(err)
		}
	}
	b.Run("full-projections", func(b *testing.B) {
		// Baseline includes the complete node-bearing projections.
		b.ReportAllocs()
		for b.Loop() {
			if _, _, _, err := loadSubscriptions(root); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("schedule", func(b *testing.B) {
		runtime := sub.Runtime{Sources: map[string]sub.SourceCache{"remote": {Nodes: nodes, Next: time.Now().Add(time.Hour)}}}
		if err := AtomicJSON(filepath.Join(root, subscriptionsSchedule), subscriptionDeadlines(config, runtime)); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := loadSubscriptionSchedule(root); err != nil {
				b.Fatal(err)
			}
		}
	})
}
