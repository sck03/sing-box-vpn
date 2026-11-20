package deployment

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneUpgradesPreservesFailuresAndUnownedDirectories(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(parent, "app")
	create := func(name string, owned, success bool, age time.Duration) string {
		p := home + ".update." + name
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
		if owned {
			if err := os.WriteFile(filepath.Join(p, ".upgrade-owned"), []byte("sing-box-vpn upgrade\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if success {
			marker := filepath.Join(p, ".upgrade-success")
			if err := os.WriteFile(marker, []byte("sing-box-vpn upgrade succeeded\n"), 0600); err != nil {
				t.Fatal(err)
			}
			at := time.Now().Add(-age)
			if err := os.Chtimes(marker, at, at); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	oldest := create("old", true, true, 3*time.Hour)
	second := create("second", true, true, 2*time.Hour)
	newest := create("new", true, true, time.Hour)
	failed := create("failed", true, false, 0)
	unknown := create("unknown", false, true, 4*time.Hour)
	outside := filepath.Join(parent, "unrelated")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	link := home + ".update.link"
	// Creating symlinks can require developer mode on Windows.
	linked := os.Symlink(outside, link) == nil
	if err := PruneUpgrades(home, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Fatal("old successful backup retained", err)
	}
	for _, p := range []string{second, newest, failed, unknown, outside} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal("removed protected path", p, err)
		}
	}
	if linked {
		if _, err := os.Lstat(link); err != nil {
			t.Fatal("removed unowned link", err)
		}
	}
	if err := PruneUpgrades(home, 0); err == nil {
		t.Fatal("accepted removal of all rollback copies")
	}
}
