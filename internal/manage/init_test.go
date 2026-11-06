package manage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitRejectsUnmanagedServerBeforeWriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sb.json")
	const original = `{"inbounds":[]}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	err := RunConfig(context.Background(), []string{"--root", root, "--core", "missing-core", "init", "--address", "127.0.0.1"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "不支持旧版迁移") {
		t.Fatal("unmanaged configuration was not rejected first", err)
	}
	if string(readBytes(t, path)) != original {
		t.Fatal("unmanaged server changed")
	}
	for _, name := range []string{"app.json", "cert.pem", "private.key", "migration-backup.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected initialization output %s: %v", name, err)
		}
	}
}

func TestInitRejectsInvalidAddressBeforeWriting(t *testing.T) {
	root := t.TempDir()
	err := initializeApp(context.Background(), root, "missing-core", "bad/address", "test")
	if err == nil || !strings.Contains(err.Error(), "服务器地址无效") {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("invalid address created configuration files", err)
	}
}

func TestInitIsIdempotent(t *testing.T) {
	root, core := t.TempDir(), coreBinary(t)
	args := []string{"--root", root, "--core", core, "init", "--address", "127.0.0.1", "--name", "test"}
	if err := RunConfig(context.Background(), args, io.Discard); err != nil {
		t.Fatal(err)
	}
	before := string(readBytes(t, filepath.Join(root, "app.json")))
	if err := RunConfig(context.Background(), args, io.Discard); err != nil {
		t.Fatal(err)
	}
	if string(readBytes(t, filepath.Join(root, "app.json"))) != before {
		t.Fatal("reinitialization changed ports, credentials or settings")
	}
}
