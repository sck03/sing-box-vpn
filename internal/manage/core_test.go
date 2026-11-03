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
	"reflect"
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
	for _, version := range []string{"1.10.7", "1.13.0", "1.15.0-alpha.1"} {
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

type fakeServices struct {
	root  string
	fail  bool
	calls []string
}

func (s *fakeServices) active(context.Context) ([]string, error) { return []string{"sing-box"}, nil }
func (s *fakeServices) restart(_ context.Context, name string) error {
	s.calls = append(s.calls, name)
	data, err := os.ReadFile(filepath.Join(s.root, "sing-box"))
	if err != nil {
		return err
	}
	if s.fail && string(data) == "new" {
		return errors.New("startup failed")
	}
	return nil
}
func updaterFixture(t *testing.T) (string, string, *coreUpdater, *fakeServices) {
	t.Helper()
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	for name, content := range map[string]string{"sing-box": "old", "candidate": "new", "sb.json": `{"credentials":"unchanged"}`} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	services := &fakeServices{root: root}
	u := newCoreUpdater()
	u.services = services
	u.check = func(context.Context, string) (string, error) { return "1.15.0", nil }
	u.validate = func(context.Context, string, string) error { return nil }
	return root, candidate, u, services
}
func TestIncompatibleCoreNeverReplacesCurrent(t *testing.T) {
	root, candidate, u, _ := updaterFixture(t)
	u.validate = func(context.Context, string, string) error { return errors.New("incompatible") }
	if _, err := u.activate(context.Background(), root, candidate, true); err == nil {
		t.Fatal("incompatible core installed")
	}
	if string(readBytes(t, filepath.Join(root, "sing-box"))) != "old" {
		t.Fatal("old core changed")
	}
}
func TestCoreUpdateAndAutomaticRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rollback"}[fail], func(t *testing.T) {
			root, candidate, u, services := updaterFixture(t)
			services.fail = fail
			before := readBytes(t, filepath.Join(root, "sb.json"))
			_, err := u.activate(context.Background(), root, candidate, true)
			expected, calls := "new", []string{"sing-box"}
			if fail {
				expected, calls = "old", []string{"sing-box", "sing-box"}
				if err == nil {
					t.Fatal("startup failure ignored")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if string(readBytes(t, filepath.Join(root, "sing-box"))) != expected || !bytes.Equal(before, readBytes(t, filepath.Join(root, "sb.json"))) || !reflect.DeepEqual(services.calls, calls) {
				t.Fatal("update altered config or restarted wrong services")
			}
			if string(readBytes(t, filepath.Join(root, "sing-box.previous"))) != "old" {
				t.Fatal("missing previous core")
			}
		})
	}
}
func TestRollbackKeepsReplacedCoreAndStagedCandidateIsImmutable(t *testing.T) {
	root, _, u, _ := updaterFixture(t)
	previous := filepath.Join(root, "sing-box.previous")
	if err := os.WriteFile(previous, []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	u.validate = func(_ context.Context, _, candidate string) error {
		if string(readBytes(t, candidate)) != "previous" {
			t.Fatal("wrong staged core")
		}
		return os.WriteFile(previous, []byte("changed-during-check"), 0600)
	}
	if _, err := u.activate(context.Background(), root, previous, false); err != nil {
		t.Fatal(err)
	}
	if string(readBytes(t, filepath.Join(root, "sing-box"))) != "previous" || string(readBytes(t, previous)) != "old" {
		t.Fatal("rollback used a modified input or lost replaced core")
	}
}
func TestConfigChangesDuringValidationAreDetected(t *testing.T) {
	root, _, _, _ := updaterFixture(t)
	err := validateConfigs(context.Background(), root, "candidate", func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, os.WriteFile(filepath.Join(root, "sb.json"), []byte(`{}`), 0600)
	})
	if err == nil {
		t.Fatal("concurrent config edit was accepted")
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
