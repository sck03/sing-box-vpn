package manage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNullVPNSecretsReturnsErrorWithoutWriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "vpngate", "secrets.json")
	if err := AtomicBytes(path, []byte("null\n"), 0600); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if failure := recover(); failure != nil {
			t.Fatalf("invalid secrets caused panic instead of an error: %v", failure)
		}
	}()
	if _, err := PrepareVPN(context.Background(), root, "missing-core", App{}, "JP"); err == nil {
		t.Fatal("null secrets accepted")
	}
	if string(readBytes(t, path)) != "null\n" {
		t.Fatal("invalid secrets overwritten")
	}
	if _, err := os.Stat(filepath.Join(root, "cert.pem")); !os.IsNotExist(err) {
		t.Fatal("invalid secrets created certificates before rejection", err)
	}
}

func TestNullAppRejectedBeforeCoreCheck(t *testing.T) {
	root := t.TempDir()
	if err := AtomicBytes(filepath.Join(root, "app.json"), []byte("null"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RunConfig(context.Background(), []string{"--root", root, "show", "--format", "text"}, io.Discard); err == nil {
		t.Fatal("null app displayed as a valid configuration")
	}
}
