package manage

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestSubscriptionProjectionsExcludePayloadsAndSecrets(t *testing.T) {
	c := sub.DefaultConfig()
	c.Sources = []sub.Source{{ID: "remote", Kind: "remote", URL: "https://private.example.com/secret", Format: "auto"}}
	c.Templates[0].URL = "https://private.example.com/template?token=private-token"
	c.Outputs = []sub.Output{{ID: "output", Name: "Public label", Template: c.Templates[0].ID}}
	c.Deliveries = []sub.Delivery{{ID: "destination", Token: "private-token", Password: "private-password", URL: "https://private.example.com", Automatic: true}}
	next := time.Now().Add(time.Hour).UTC()
	r := sub.Runtime{Sources: map[string]sub.SourceCache{"remote": {Next: next, Nodes: []sub.Node{{Password: "node-password"}}}}}
	snapshot := sub.Snapshot{Artifacts: map[string]sub.Artifact{"output": {Data: []byte("private-node-payload")}}, NodeCounts: map[string]int{"output": 2}, Diagnostics: map[string][]sub.Diagnostic{"output": {{Stage: "validation", Message: "core-check"}}}}
	snapshot.Validations = map[string]sub.Validation{"output": {State: "passed", Engine: "sing-box", Scope: "core-config"}}
	schedule, outputs := subscriptionDeadlines(c, r), outputStatus(c, snapshot)
	for _, projection := range []any{schedule, outputs} {
		data, err := json.Marshal(projection)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"private.example", "private-token", "private-password", "node-password", "private-node-payload"} {
			if strings.Contains(string(data), secret) {
				t.Fatal("projection leaked", secret)
			}
		}
	}
	if len(outputs) != 1 || outputs[0].Nodes != 2 || !outputs[0].Available || outputs[0].Validation.State != "passed" || outputs[0].Validation.Engine != "sing-box" {
		t.Fatal(outputs)
	}
	root := t.TempDir()
	if err := AtomicJSON(filepath.Join(root, subscriptionsSchedule), schedule); err != nil {
		t.Fatal(err)
	}
	// Scheduling consumes the dedicated projection even when a large payload is
	// unreadable; it never opens the cache on the normal path.
	if err := AtomicBytes(filepath.Join(root, subscriptionsRuntime), []byte("broken payload"), 0600); err != nil {
		t.Fatal(err)
	}
	config, deadlines, err := loadSubscriptionSchedule(root)
	if err != nil || len(config.Sources) != 1 || !deadlines.Sources["remote"].Next.Equal(next) {
		t.Fatal(config, deadlines, err)
	}
}
