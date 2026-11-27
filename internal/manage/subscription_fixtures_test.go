package manage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// Compiler fixtures explicitly create the same confirmed state as installation.
// Production refresh never accepts missing generation metadata for test purposes.
func confirmSubscriptionFixture(t *testing.T, root string) {
	t.Helper()
	config, _, err := loadSubscriptionState(root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := controlstore.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, err := store.State(t.Context())
	var platform PlatformConfig
	revision := int64(1)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := ReadJSON(filepath.Join(root, "app.json"), &platform.App); err != nil {
			t.Fatal(err)
		}
		platform.Schema = PlatformSchema
		platform.Subscriptions = config
		platform.normalize()
		if err := store.Initialize(t.Context(), mustJSON(t, platform)); err != nil {
			t.Fatal(err)
		}
	} else {
		if err != nil {
			t.Fatal(err)
		}
		platform, err = decodePlatform(state.Applied)
		if err != nil {
			t.Fatal(err)
		}
		platform.Subscriptions = config
		id, err := store.Queue(t.Context(), "apply", domain.TaskSubmission{Actor: "fixture", Source: domain.TaskUser}, []byte("{}"), state.Revision, mustJSON(t, platform))
		if err != nil {
			t.Fatal(err)
		}
		revision = state.Revision + 1
		if err := store.Applied(t.Context(), revision); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(t.Context(), id, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := AtomicJSON(filepath.Join(root, "runtime-revision.json"), revision); err != nil {
		t.Fatal(err)
	}
}

// Explicit inline fixtures replace the removed shared-local source format.
func fixtureSubscriptionConfig() sub.Config {
	c := sub.DefaultConfig()
	for _, scope := range []string{normalScope, vpngateScope} {
		c.Sources = append(c.Sources, sub.Source{ID: scope, Name: scope, Kind: "inline", Format: "nodes", Content: "[]", Required: scope == normalScope})
		c.Collections = append(c.Collections, sub.Collection{ID: scope, Sources: []string{scope}, Processor: sub.Processor{Deduplicate: true}})
		for _, suffix := range []string{"sing-box", "mihomo", "provider", "base64", "uri"} {
			id := scope + "-" + suffix
			c.Outputs = append(c.Outputs, sub.Output{ID: id, Name: id, Collection: scope, Template: id})
		}
	}
	return c
}
