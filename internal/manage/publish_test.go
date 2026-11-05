package manage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writePendingJournal(t *testing.T, root string, before fileBundle, stage string) {
	t.Helper()
	j := commitJournal{Files: map[string]string{}, Stage: stage}
	if err := os.MkdirAll(filepath.Join(root, stage), 0700); err != nil {
		t.Fatal(err)
	}
	if err := AtomicBytes(filepath.Join(root, stage, ".owner"), []byte(snapshotOwner), 0600); err != nil {
		t.Fatal(err)
	}
	for name, data := range before {
		j.Files[name] = ""
		if data != nil {
			if err := AtomicBytes(filepath.Join(root, stage, "before", name), data, 0600); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			j.Files[name] = hex.EncodeToString(digest[:])
		}
	}
	if err := AtomicJSON(filepath.Join(root, journalName), j); err != nil {
		t.Fatal(err)
	}
}

func TestPublishSkipsUnchangedFiles(t *testing.T) {
	root := t.TempDir()
	files := fileBundle{"app.json": []byte("state"), "sbox.json": []byte("old export")}
	if err := publish(root, files); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "app.json")
	old := time.Unix(1700000000, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	files["sbox.json"] = []byte("new export")
	files["empty.txt"] = []byte{}
	files["missing.json"] = nil
	if err := publish(root, files); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(old) {
		t.Fatal("unchanged state was rewritten", err)
	}
	if string(readBytes(t, filepath.Join(root, "sbox.json"))) != "new export" {
		t.Fatal("changed export was not published")
	}
	if data := readBytes(t, filepath.Join(root, "empty.txt")); len(data) != 0 {
		t.Fatal("empty file was confused with deletion")
	}
	if _, err = os.Stat(filepath.Join(root, "missing.json")); !os.IsNotExist(err) {
		t.Fatal("missing file was recreated", err)
	}
}

func TestInterruptedPublishRestoresBeforeNextManagementOperation(t *testing.T) {
	root := t.TempDir()
	before := fileBundle{"app.json": []byte("old state"), "sb.json": []byte("old server"), "vpngate/sbox.json": []byte("old export"), "empty.txt": {}, "new.txt": nil}
	if err := publish(root, fileBundle{"app.json": []byte("partial new state"), "sb.json": []byte("partial new server"), "new.txt": []byte("partial new file")}); err != nil {
		t.Fatal(err)
	}
	stage := snapshotStage
	if err := os.Mkdir(filepath.Join(root, stage), 0700); err != nil {
		t.Fatal(err)
	}
	writePendingJournal(t, root, before, stage)
	if err := withLock(root, func() error {
		for name, want := range before {
			path := filepath.Join(root, name)
			if want == nil {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("interrupted new file survived: %s, %v", name, err)
				}
			} else if !bytes.Equal(want, readBytes(t, path)) {
				t.Fatalf("management observed a partial configuration: %s", name)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".pending-config.json", stage} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatal("completed recovery retained transaction files", name, err)
		}
	}
	if err := withLock(root, func() error { return nil }); err != nil {
		t.Fatal("recovery was not idempotent", err)
	}
}

func TestRecoveryRejectsInvalidJournalBeforeWriting(t *testing.T) {
	for _, bad := range []Object{
		{"files": map[string]string{"../outside.txt": ""}, "stage": ".commit-test"},
		{"files": map[string]string{"app.json": ""}, "stage": "../outside"},
		{"files": map[string]string{".manage.lock": ""}, "stage": ".commit-test"},
		{"files": map[string]string{"other/../.manage.lock": ""}, "stage": ".commit-test"},
	} {
		root := t.TempDir()
		if err := AtomicBytes(filepath.Join(root, "app.json"), []byte("unchanged"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := AtomicJSON(filepath.Join(root, ".pending-config.json"), bad); err != nil {
			t.Fatal(err)
		}
		called := false
		if err := withLock(root, func() error { called = true; return nil }); err == nil || called {
			t.Fatal("invalid recovery metadata was ignored")
		}
		if string(readBytes(t, filepath.Join(root, "app.json"))) != "unchanged" {
			t.Fatal("invalid journal changed the current configuration")
		}
	}
}

func TestFailedRecoveryKeepsJournalForRetry(t *testing.T) {
	root := t.TempDir()
	if err := AtomicBytes(filepath.Join(root, "vpngate"), []byte("blocking file"), 0600); err != nil {
		t.Fatal(err)
	}
	before := fileBundle{"app.json": []byte("old state"), "vpngate/sbox.json": []byte("old export")}
	journal := filepath.Join(root, ".pending-config.json")
	writePendingJournal(t, root, before, snapshotStage)
	if err := withLock(root, func() error { t.Fatal("failed recovery continued into mutation"); return nil }); err == nil {
		t.Fatal("recovery ignored an unwritable destination")
	}
	if _, err := os.Stat(journal); err != nil {
		t.Fatal("failed recovery lost its backup", err)
	}
	if err := os.Rename(filepath.Join(root, "vpngate"), filepath.Join(root, "blocking-file.saved")); err != nil {
		t.Fatal(err)
	}
	if err := withLock(root, func() error { return nil }); err != nil {
		t.Fatal("recovery could not retry after repairing the directory", err)
	}
	for name, want := range before {
		if !bytes.Equal(readBytes(t, filepath.Join(root, name)), want) {
			t.Fatal("retry did not restore the complete configuration", name)
		}
	}
}

func TestCleanupLockDoesNotRequireValidRecoveryState(t *testing.T) {
	root := t.TempDir()
	if err := AtomicBytes(filepath.Join(root, ".pending-config.json"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := withFileLock(root, func() error { called = true; return nil }); err != nil || !called {
		t.Fatal("damaged recovery metadata blocked confirmed cleanup", err)
	}
	if err := withLock(root, func() error { return nil }); err == nil {
		t.Fatal("normal mutations skipped damaged recovery metadata")
	}
}

func TestRecoverWaitsForCurrentConfigurationOperation(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	if err := withFileLock(root, func() error {
		go func() { done <- RunConfig(ctx, []string{"--root", root, "recover"}, &bytes.Buffer{}) }()
		select {
		case err := <-done:
			t.Fatal("concurrent service recovery failed before the writer released its lock", err)
		case <-time.After(150 * time.Millisecond):
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("service recovery did not proceed after the writer finished")
	}
}

func BenchmarkPublishUnchanged(b *testing.B) {
	root := b.TempDir()
	files := fileBundle{}
	for _, name := range []string{"app.json", "sb.json", "sbox.json", "clmi.yaml", "share.json", "jhsub.txt", "jhsub-base64.txt"} {
		files[name] = bytes.Repeat([]byte("configuration\n"), 512)
	}
	if err := publish(root, files); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := publish(root, files); err != nil {
			b.Fatal(err)
		}
	}
}
