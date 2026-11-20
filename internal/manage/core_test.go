package manage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func byteResponse(data []byte) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data))}
}

func testRelease(version, digest string) coreRelease {
	name := "sing-box-" + version + "-linux-amd64.tar.gz"
	return coreRelease{Tag: "v" + version, Assets: []releaseAsset{{Name: name, URL: "https://github.com/SagerNet/sing-box/releases/download/v" + version + "/" + name, Digest: digest}}}
}

func TestStableCoreAssetValidation(t *testing.T) {
	for _, version := range []string{"1.10.7", "1.13.0", "1.15.0-alpha.1", "2.4294967296.0", "1.15.4294967296"} {
		if _, err := stableVersion(version); err == nil {
			t.Fatal("accepted", version)
		}
	}
	if asset, err := selectAsset(testRelease("1.15.0", "sha256:"+strings.Repeat("a", 64)), "amd64"); err != nil || asset.Version != "1.15.0" {
		t.Fatal("future stable version locked to baseline", err)
	}
	for _, change := range []func(*coreRelease){func(r *coreRelease) { r.Prerelease = true }, func(r *coreRelease) { r.Draft = true }, func(r *coreRelease) { r.Assets[0].URL = "https://other.example/core" }, func(r *coreRelease) { r.Assets[0].Digest = "" }} {
		r := testRelease("1.15.0", "sha256:"+strings.Repeat("a", 64))
		change(&r)
		if _, err := selectAsset(r, "amd64"); err == nil {
			t.Fatal("accepted unverified release")
		}
	}
	r := testRelease("1.14.4", "sha256:invalid")
	if _, err := selectAsset(r, "amd64"); err == nil {
		t.Fatal("invalid published hash bypassed with baseline fallback")
	}
}

func TestBadHashAndArchiveLinkKeepCandidate(t *testing.T) {
	for _, maliciousLink := range []bool{false, true} {
		t.Run(map[bool]string{false: "hash", true: "link"}[maliciousLink], func(t *testing.T) {
			root := t.TempDir()
			candidate := filepath.Join(root, "candidate")
			if err := os.WriteFile(candidate, []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
			data, digest := []byte("bad archive"), strings.Repeat("a", 64)
			if maliciousLink {
				var archive bytes.Buffer
				gz := gzip.NewWriter(&archive)
				writer := tar.NewWriter(gz)
				if err := writer.WriteHeader(&tar.Header{Name: "sing-box-1.15.0-linux-amd64/sing-box", Typeflag: tar.TypeSymlink, Linkname: "/etc/shadow"}); err != nil {
					t.Fatal(err)
				}
				writer.Close()
				gz.Close()
				data = archive.Bytes()
				hash := sha256.Sum256(data)
				digest = hex.EncodeToString(hash[:])
			}
			asset, err := selectAsset(testRelease("1.15.0", "sha256:"+digest), "amd64")
			if err != nil {
				t.Fatal(err)
			}
			u := newCoreUpdater()
			u.client.Transport = testTransport(func(*http.Request) (*http.Response, error) { return byteResponse(data), nil })
			u.check = func(context.Context, string) (string, error) { return "1.15.0", nil }
			if err := u.download(context.Background(), asset, candidate); err == nil {
				t.Fatal("bad download accepted")
			}
			if string(readBytes(t, candidate)) != "existing" {
				t.Fatal("unverified download replaced candidate")
			}
		})
	}
}

func TestConfigurationAndCoreUpdatesShareOneLock(t *testing.T) {
	root := t.TempDir()
	if err := withLock(root, func() error {
		if err := withLock(root, func() error { return nil }); err == nil {
			t.Fatal("two mutations held the same lock")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := withLock(root, func() error { return nil }); err != nil {
		t.Fatal("lock was not released", err)
	}
}
