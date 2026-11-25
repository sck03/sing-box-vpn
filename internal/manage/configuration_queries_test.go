package manage

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/controlstore"
)

func TestConfigurationViewTracksConfirmedRevision(t *testing.T) {
	store, err := controlstore.Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	config := PlatformConfig{App: App{Name: "first"}}
	if err := store.Initialize(t.Context(), mustJSON(t, config)); err != nil {
		t.Fatal(err)
	}
	var cache configurationCache
	first, revisions, err := cache.read(t.Context(), store)
	if err != nil || first.App.Name != "first" || revisions.AppliedRevision != 1 {
		t.Fatal(first, revisions, err)
	}
	config.App.Name = "next"
	task, err := store.Queue(t.Context(), "apply", "test", []byte("{}"), 1, mustJSON(t, config))
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := store.AppliedViewSince(t.Context(), 1)
	if err != nil || len(unchanged.Config) != 0 || unchanged.Revision != 2 || unchanged.AppliedRevision != 1 {
		t.Fatal(unchanged, err)
	}
	pending, revisions, err := cache.read(t.Context(), store)
	if err != nil || pending.App.Name != "first" || revisions.Revision != 2 {
		t.Fatal(pending, revisions, err)
	}
	if err := store.Applied(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(t.Context(), task, nil); err != nil {
		t.Fatal(err)
	}
	next, _, err := cache.read(t.Context(), store)
	if err != nil || next.App.Name != "next" || first.App.Name != "first" {
		t.Fatal(next, first, err)
	}
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			for range 8 {
				got, state, err := cache.read(t.Context(), store)
				if err != nil || got.App.Name != "next" || state.AppliedRevision != 2 {
					t.Error("inconsistent concurrent snapshot", got.App.Name, state, err)
				}
			}
		})
	}
	readers.Wait()
}

func TestPanelPortReservationIncludesActiveAndSaved(t *testing.T) {
	s := &PlatformService{Root: t.TempDir()}
	if err := AtomicJSON(filepath.Join(s.Root, "panel.json"), PanelOptions{Listen: "127.0.0.1:2099"}); err != nil {
		t.Fatal(err)
	}
	s.Handler(PanelOptions{Listen: "127.0.0.1:2096"})
	for _, port := range []int{2096, 2099, 2100} {
		config := PlatformConfig{App: App{Advanced: CoreSettings{Services: []Object{{"listen_port": port}}}}}
		if err := s.validateConfiguredPanelPort(config); (err == nil) != (port == 2100) {
			t.Fatalf("port=%d, error=%v", port, err)
		}
	}
	s.Handler(PanelOptions{Listen: "127.0.0.1:2099"})
	if err := s.validateConfiguredPanelPort(PlatformConfig{App: App{Advanced: CoreSettings{Services: []Object{{"listen_port": 2096}}}}}); err != nil {
		t.Fatal("restarted listener retained old port", err)
	}
}
