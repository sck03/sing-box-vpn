package hostservice

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnitPreflightRejectsForeignFiles(t *testing.T) {
	root := t.TempDir()
	owned, foreign := filepath.Join(root, "owned.service"), filepath.Join(root, "foreign.service")
	if err := os.WriteFile(owned, []byte(UnitOwner+"[Service]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("# another program\nDescription=sing-box-vpn\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckUnitOwnership([]string{owned, filepath.Join(root, "new.service")}, ReadUnit); err != nil {
		t.Fatal(err)
	}
	if err := CheckUnitOwnership([]string{owned, foreign}, ReadUnit); err == nil {
		t.Fatal("foreign unit was accepted")
	}
	link := filepath.Join(root, "link.service")
	if err := os.Symlink(owned, link); err != nil {
		t.Log("symlink creation unavailable:", err)
		return
	}
	if err := CheckUnitOwnership([]string{link}, ReadUnit); err == nil {
		t.Fatal("symlink was accepted")
	}
}
