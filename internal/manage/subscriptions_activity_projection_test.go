package manage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestSubscriptionStatusUsesSmallSecretFreeProjection(t *testing.T) {
	root, _, _ := appFixture(t)
	config, runtime, err := loadSubscriptionState(root)
	if err != nil {
		t.Fatal(err)
	}
	destination := sub.Delivery{ID: "device", Kind: "http-put", Output: "normal-provider", URL: "https://secret.example/upload", Token: "private-token", Automatic: true}
	config.Deliveries = []sub.Delivery{destination}
	now := time.Now().UTC()
	runtime.Deliveries = map[string]sub.DeliveryState{"device": {ConfigHash: sub.DeliveryHash(destination), Digest: "private-digest", Status: "uncertain", Checked: now, Success: now.Add(-time.Hour), Next: now.Add(time.Minute), Failures: 2, Error: "remote acceptance unknown"}}
	if err := writeSubscriptionRuntime(root, config, runtime); err != nil {
		t.Fatal(err)
	}
	// Reading operational status must not parse either large authoritative input.
	for _, name := range []string{subscriptionsRuntime, subscriptionsConfig} {
		if err := AtomicBytes(filepath.Join(root, name), []byte("{broken"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	service := PlatformService{Root: root}
	status, err := service.SubscriptionStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Deliveries) != 1 || status.Deliveries[0].State != "uncertain" || status.Deliveries[0].Failures != 2 || status.Deliveries[0].Next != now.Add(time.Minute) {
		t.Fatal(status)
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret.example", "private-token", "private-digest", "config_hash"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("status leaks", secret)
		}
	}
}

func BenchmarkSubscriptionActivityRead(b *testing.B) {
	root := b.TempDir()
	config := sub.DefaultConfig()
	runtime := sub.Runtime{Sources: map[string]sub.SourceCache{}}
	now := time.Now().UTC()
	for i := range 64 {
		source := sub.Source{ID: fmt.Sprintf("source-%d", i), Name: "Remote source", Kind: "remote", URL: "https://example.test/subscription", Format: "uri"}
		config.Sources = append(config.Sources, source)
		cached := sub.SourceCache{SourceHash: sub.SourceHash(source), Checked: now, Success: now, Next: now.Add(time.Hour)}
		for j := range 64 {
			cached.Nodes = append(cached.Nodes, sub.Node{ID: fmt.Sprintf("node-%d-%d", i, j), Name: "Remote node", Protocol: "trojan", Server: "example.test", Port: 443, Password: strings.Repeat("x", 32)})
		}
		runtime.Sources[source.ID] = cached
	}
	if err := writeSubscriptionRuntime(root, config, runtime); err != nil {
		b.Fatal(err)
	}
	for _, name := range []string{"runtime-scan", "activity-projection"} {
		b.Run(name, func(b *testing.B) {
			file := subscriptionsRuntime
			if name == "activity-projection" {
				file = subscriptionsActivity
			}
			stat, err := os.Stat(filepath.Join(root, file))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				// Model the old metadata-only decode: JSON still scans every cached node.
				var previous struct {
					Sources map[string]struct {
						Checked, Success, Next time.Time
						SourceHash             string `json:"source_hash"`
					}
				}
				var projection subscriptionActivityProjection
				var target any = &previous
				if name == "activity-projection" {
					target = &projection
				}
				if _, err := ReadJSON(filepath.Join(root, file), target); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(stat.Size()), "file-bytes")
		})
	}
}
