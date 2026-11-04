package manage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
