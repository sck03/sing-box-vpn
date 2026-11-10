package manage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func coreArchive(t testing.TB, duplicate bool, payload string) []byte {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	files := []struct{ name, data string }{
		{"LICENSE", "license"},
		{"sing-box-1.15.0-linux-amd64/sing-box", payload},
		{"README", "documentation after the binary"},
	}
	if duplicate {
		files = append(files, files[1])
	}
	for _, file := range files {
		if err := tw.WriteHeader(&tar.Header{Name: file.name, Mode: 0755, Size: int64(len(file.data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestCoreDownloadValidatesCompleteArchive(t *testing.T) {
	for _, mode := range []string{"valid", "truncated-gzip", "bad-crc", "duplicate-core", "canceled-check"} {
		t.Run(mode, func(t *testing.T) {
			data := coreArchive(t, mode == "duplicate-core", "core-binary")
			switch mode {
			case "truncated-gzip":
				data = data[:len(data)-8]
			case "bad-crc":
				data[len(data)-8] ^= 0xff
			}
			hash := sha256.Sum256(data)
			asset := verifiedAsset{Version: "1.15.0", Arch: "amd64", URL: "https://example.invalid/core", SHA256: hex.EncodeToString(hash[:])}
			root := t.TempDir()
			path := filepath.Join(root, "candidate")
			if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			u := newCoreUpdater()
			u.client.Transport = testTransport(func(*http.Request) (*http.Response, error) { return byteResponse(data), nil })
			checked := false
			u.check = func(_ context.Context, binary string) (string, error) {
				checked = true
				if string(readBytes(t, binary)) != "core-binary" {
					t.Fatal("wrong extracted core")
				}
				if mode == "canceled-check" {
					cancel()
				}
				return "1.15.0", nil
			}
			err := u.download(ctx, asset, path)
			if mode == "valid" {
				if err != nil || !checked || string(readBytes(t, path)) != "core-binary" {
					t.Fatalf("valid download failed: %v", err)
				}
			} else {
				if err == nil || string(readBytes(t, path)) != "existing" {
					t.Errorf("invalid or canceled download replaced candidate: %v", err)
				}
				if mode == "canceled-check" {
					if !errors.Is(err, context.Canceled) {
						t.Errorf("cancellation lost: %v", err)
					}
				} else if checked {
					t.Error("core check executed before archive integrity was verified")
				}
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 1 || entries[0].Name() != "candidate" {
				t.Fatalf("download left staging files: %v, %v", entries, err)
			}
		})
	}
}

func BenchmarkCoreDownload(b *testing.B) {
	data := coreArchive(b, false, string(bytes.Repeat([]byte("core"), 1<<20)))
	hash := sha256.Sum256(data)
	asset := verifiedAsset{Version: "1.15.0", Arch: "amd64", URL: "https://example.invalid/core", SHA256: hex.EncodeToString(hash[:])}
	u := newCoreUpdater()
	u.client.Transport = testTransport(func(*http.Request) (*http.Response, error) { return byteResponse(data), nil })
	u.check = func(context.Context, string) (string, error) { return "1.15.0", nil }
	path := filepath.Join(b.TempDir(), "candidate")
	b.ReportAllocs()
	b.SetBytes(4 << 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := u.download(context.Background(), asset, path); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCoreArchiveRejectsOversizedSkippedEntry(t *testing.T) {
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	// A declared oversized non-core entry must fail before trying to consume
	// its body; a small compressed download must not buy unlimited expansion.
	if err := tw.WriteHeader(&tar.Header{Name: "unused", Size: maxCoreExpanded + 1}); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	err := extractCore(context.Background(), &data, io.Discard, "sing-box")
	if err == nil || !strings.Contains(err.Error(), "预算") {
		t.Fatalf("oversized skipped entry was consumed: %v", err)
	}
}

func TestArchiveReaderBoundsAndCancellation(t *testing.T) {
	for _, size := range []int{7, 8, 9} {
		source := strings.NewReader(strings.Repeat("x", size))
		r := &archiveReader{ctx: context.Background(), source: source, remaining: 8}
		_, err := io.ReadAll(r)
		if (err == nil) != (size <= 8) {
			t.Fatalf("size %d: %v", size, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	source := strings.NewReader("0123456789")
	r := &archiveReader{ctx: ctx, source: source, remaining: 8}
	var first [4]byte
	if _, err := io.ReadFull(r, first[:]); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := io.ReadAll(r); !errors.Is(err, context.Canceled) || source.Len() != 6 {
		t.Fatalf("read continued after cancellation: remaining=%d, err=%v", source.Len(), err)
	}
}

type cancelCoreWriter struct{ cancel context.CancelFunc }

func (w cancelCoreWriter) Write(p []byte) (int, error) { w.cancel(); return len(p), nil }

func TestCoreArchiveCancellationDuringExtraction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	data := coreArchive(t, false, "core-binary")
	err := extractCore(ctx, bytes.NewReader(data), cancelCoreWriter{cancel}, "sing-box-1.15.0-linux-amd64/sing-box")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("extraction continued after cancellation: %v", err)
	}
}

type unreadAssetBody struct {
	read, closed bool
}

func (b *unreadAssetBody) Read([]byte) (int, error) { b.read = true; return 0, io.EOF }
func (b *unreadAssetBody) Close() error             { b.closed = true; return nil }

func TestVerifiedAssetRejectsOversizedContentLength(t *testing.T) {
	body := &unreadAssetBody{}
	client := &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: maxAssetSize + 1, Body: body}, nil
	})}
	asset := releaseAsset{URL: "https://example.invalid/core", Digest: "sha256:" + strings.Repeat("a", 64)}
	err := copyVerifiedAsset(context.Background(), client, asset, io.Discard)
	if err == nil || body.read || !body.closed {
		t.Fatalf("oversized body was not rejected and closed: read=%t, closed=%t, err=%v", body.read, body.closed, err)
	}
}
