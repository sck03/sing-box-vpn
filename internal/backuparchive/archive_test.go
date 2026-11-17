package backuparchive

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthenticatedRoundTripAndDamage(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "db")
	data := bytes.Repeat([]byte("database payload"), 10000)
	if err := os.WriteFile(database, data, 0600); err != nil {
		t.Fatal(err)
	}
	var encrypted bytes.Buffer
	password := []byte("roundtrip-test-password")
	if err := Write(t.Context(), &encrypted, password, Manifest{Root: root}, database, nil); err != nil {
		t.Fatal(err)
	}
	decoded, err := Read(encrypted.Bytes(), password)
	if err != nil || !bytes.Equal(data, decoded.Database) || decoded.Root != root {
		t.Fatal(err)
	}
	for _, index := range []int{len(Magic) + 5, encrypted.Len() / 2, encrypted.Len() - 1} {
		bad := bytes.Clone(encrypted.Bytes())
		bad[index] ^= 1
		if _, err := Read(bad, password); err == nil {
			t.Fatalf("accepted damaged byte %d", index)
		}
	}
	if _, err := Read(encrypted.Bytes()[:encrypted.Len()-20], password); err == nil {
		t.Fatal("accepted truncation")
	}
	if _, err := Read(encrypted.Bytes(), []byte("incorrect-password")); err == nil {
		t.Fatal("accepted wrong password")
	}
}

func TestWriterRejectsEscapingMaterial(t *testing.T) {
	for _, name := range []string{"../escape.pem", "/etc/key.pem", "C:/key.pem", "a/../key.pem", "a\\key.pem", "a.txt"} {
		if validFile(name) {
			t.Fatalf("accepted %q", name)
		}
	}
}
