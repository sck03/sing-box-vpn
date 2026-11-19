package hostservice

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestJournalCommandsAreNamespaceScoped(t *testing.T) {
	var commands [][]string
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		return nil, nil
	}
	if err := ClearJournal(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"journalctl", "--namespace=sb-vpn", "--rotate"}, {"journalctl", "--namespace=sb-vpn", "--vacuum-size=1K"}}
	if !reflect.DeepEqual(commands, want) {
		t.Fatal(commands)
	}
	if _, err := (JournalQuery{Service: "ssh"}).Args(); err == nil {
		t.Fatal("external unit accepted")
	}
	if _, err := (JournalQuery{Service: "sb-core", Lines: 1001}).Args(); err == nil {
		t.Fatal("unbounded query accepted")
	}
}

func TestJournalOwnershipRejectsForeignAndLinkedPaths(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "limits.conf")
	if err := CheckJournalOwnership(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Another program\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckJournalOwnership(path); err == nil {
		t.Fatal("foreign config accepted")
	}
	if err := os.WriteFile(path, []byte(JournalLimits), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckJournalOwnership(path); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.conf")
	if err := os.Symlink(path, link); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	if err := CheckJournalOwnership(link); err == nil {
		t.Fatal("linked config accepted")
	}
	directory := filepath.Join(root, "linked-directory")
	if err := os.Symlink(root, directory); err != nil {
		t.Fatal(err)
	}
	if err := CheckJournalOwnership(filepath.Join(directory, "limits.conf")); err == nil {
		t.Fatal("linked directory accepted")
	}
}
