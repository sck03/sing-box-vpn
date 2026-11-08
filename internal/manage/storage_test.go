package manage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestLargeCommitRecoverySnapshot(t *testing.T) {
	root := t.TempDir()
	before := fileBundle{"app.json": bytes.Repeat([]byte{'a'}, 7<<20), "sb.json": bytes.Repeat([]byte{'b'}, 7<<20)}
	if err := publish(root, before); err != nil {
		t.Fatal(err)
	}
	j, err := preparePublish(root, fileBundle{"app.json": []byte("new app"), "sb.json": []byte("new server")})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := JSONBytes(j)
	if err != nil || len(metadata) > 4096 {
		t.Fatal("snapshot contents were embedded in the journal", len(metadata), err)
	}
	if err := AtomicJSON(filepath.Join(root, journalName), j); err != nil {
		t.Fatal(err)
	}
	if err := AtomicBytes(filepath.Join(root, "app.json"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := recoverPublish(root); err != nil {
		t.Fatal("large snapshot could not be recovered", err)
	}
	for name, want := range before {
		if !bytes.Equal(readBytes(t, filepath.Join(root, name)), want) {
			t.Fatal("large recovery changed the backup", name)
		}
	}
}

func TestAtomicJSONMatchesExportEncoding(t *testing.T) {
	value := Object{"html": "<a>&", "unicode": "日本", "items": []string{"one", "two"}}
	want, err := JSONBytes(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := AtomicJSON(path, value); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readBytes(t, path), want) {
		t.Fatal("atomic JSON changed formatting or escaping")
	}
}

func TestSnapshotDraftDoesNotAccumulate(t *testing.T) {
	root := t.TempDir()
	if err := AtomicBytes(filepath.Join(root, "app.json"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := preparePublish(root, fileBundle{"app.json": []byte("first")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := preparePublish(root, fileBundle{"app.json": []byte("second")})
	if err != nil || first.Stage != second.Stage {
		t.Fatal("draft preparation accumulated history", err)
	}
	if err := AtomicJSON(filepath.Join(root, journalName), second); err != nil {
		t.Fatal(err)
	}
	if _, err := preparePublish(root, fileBundle{"app.json": []byte("third")}); err == nil {
		t.Fatal("active snapshot was overwritten")
	}
	if err := recoverPublish(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, snapshotStage)); !os.IsNotExist(err) {
		t.Fatal("completed recovery retained a snapshot")
	}
}

func TestCorruptSnapshotDoesNotPartiallyRestore(t *testing.T) {
	root := t.TempDir()
	if err := publish(root, fileBundle{"app.json": []byte("old app"), "sb.json": []byte("old server")}); err != nil {
		t.Fatal(err)
	}
	j, err := preparePublish(root, fileBundle{"app.json": []byte("new app"), "sb.json": []byte("new server")})
	if err != nil {
		t.Fatal(err)
	}
	if err := AtomicJSON(filepath.Join(root, journalName), j); err != nil {
		t.Fatal(err)
	}
	if err := AtomicBytes(filepath.Join(root, "app.json"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicBytes(filepath.Join(root, j.Stage, "before", "sb.json"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := recoverPublish(root); err == nil {
		t.Fatal("damaged snapshot was accepted")
	}
	if string(readBytes(t, filepath.Join(root, "app.json"))) != "partial" {
		t.Fatal("valid backup was restored before checking the damaged one")
	}
	if _, err := os.Stat(filepath.Join(root, journalName)); err != nil {
		t.Fatal("damaged snapshot lost its record", err)
	}
}

func TestAtomicWriteErrorPreservesCurrentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := AtomicBytes(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("write rejected")
	err := atomicWrite(path, 0600, func(output io.Writer) error { _, _ = output.Write([]byte("partial")); return failure })
	if !errors.Is(err, failure) || string(readBytes(t, path)) != "old" {
		t.Fatal("failed atomic write changed current data", err)
	}
}

func TestAssetChecksumFailurePreservesInstalledFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloudflared")
	if err := AtomicBytes(path, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	data := []byte("new asset")
	digest := sha256.Sum256(data)
	client := &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) { return byteResponse(data), nil })}
	asset := releaseAsset{URL: "https://example.test/asset", Digest: "sha256:" + hex.EncodeToString(digest[:])}
	bad := asset
	bad.Digest = "sha256:" + string(bytes.Repeat([]byte{'0'}, 64))
	if err := downloadAsset(context.Background(), client, bad, path); err == nil {
		t.Fatal("asset with a bad checksum was installed")
	}
	if string(readBytes(t, path)) != "old" {
		t.Fatal("bad download replaced the installed asset")
	}
	if err := downloadAsset(context.Background(), client, asset, path); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readBytes(t, path), data) {
		t.Fatal("verified asset was not installed")
	}
}

func TestReadJSONLimitsAndTrailingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"value":1} {"value":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	var value Object
	if _, err := ReadJSON(path, &value); err == nil {
		t.Fatal("multiple JSON values were accepted")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxJSONFile + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	if _, err := ReadJSON(path, &value); err == nil {
		t.Fatal("oversized configuration was accepted")
	}
}

func TestReadJSONRejectsNullWithoutChangingTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(" \n null\t\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	value := Object{"keep": true}
	if found, err := ReadJSON(path, &value); !found || err == nil {
		t.Fatal("null configuration was not reported as invalid", found, err)
	}
	if value["keep"] != true {
		t.Fatal("invalid configuration cleared the destination map")
	}
}

func BenchmarkPublishChanged(b *testing.B) {
	root := b.TempDir()
	values := [2][]byte{bytes.Repeat([]byte{'a'}, 256<<10), bytes.Repeat([]byte{'b'}, 256<<10)}
	files := fileBundle{}
	i := 0
	b.ReportAllocs()
	for b.Loop() {
		for _, name := range []string{"app.json", "sb.json", "vpngate/server.json"} {
			files[name] = values[i%2]
		}
		if err := publish(root, files); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func BenchmarkAtomicJSON(b *testing.B) {
	path := filepath.Join(b.TempDir(), "state.json")
	value := Object{"value": string(bytes.Repeat([]byte{'a'}, 256<<10))}
	b.ReportAllocs()
	for b.Loop() {
		if err := AtomicJSON(path, value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadJSON(b *testing.B) {
	path := filepath.Join(b.TempDir(), "state.json")
	if err := os.WriteFile(path, append(append([]byte(`{"value":"`), bytes.Repeat([]byte{'a'}, 256<<10)...), []byte(`"}`)...), 0600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		var value Object
		if _, err := ReadJSON(path, &value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCopyFileContent(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
	}{{"8KiB", 8 << 10}, {"128KiB", 128 << 10}} {
		b.Run(tc.name, func(b *testing.B) {
			root := b.TempDir()
			data := bytes.Repeat([]byte{'x'}, tc.size)
			path := filepath.Join(root, "source")
			if err := os.WriteFile(path, data, 0600); err != nil {
				b.Fatal(err)
			}
			input, err := os.Open(path)
			if err != nil {
				b.Fatal(err)
			}
			defer input.Close()
			output, err := os.Create(filepath.Join(root, "destination"))
			if err != nil {
				b.Fatal(err)
			}
			defer output.Close()
			b.ReportAllocs()
			b.SetBytes(int64(tc.size))
			for b.Loop() {
				if _, err := input.Seek(0, io.SeekStart); err != nil {
					b.Fatal(err)
				}
				if _, err := output.Seek(0, io.SeekStart); err != nil {
					b.Fatal(err)
				}
				if n, err := copyFileContent(output, input); err != nil || n != int64(tc.size) {
					b.Fatal("copy failed", n, err)
				}
			}
		})
	}
}
