package manage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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
	root         string
	fail         bool
	calls        []string
	afterFailure func()
}

func (s *fakeServices) active(context.Context) ([]string, error) { return []string{"sing-box"}, nil }
func (s *fakeServices) restart(_ context.Context, name string) error {
	s.calls = append(s.calls, name)
	data, err := os.ReadFile(filepath.Join(s.root, "sing-box"))
	if err != nil {
		return err
	}
	if s.fail && string(data) == "new" {
		if s.afterFailure != nil {
			s.afterFailure()
		}
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

type recoveringServices struct{ fakeServices }

func (s *recoveringServices) restart(ctx context.Context, name string) error {
	// ExecStartPre must be able to recover while the update still owns the
	// operation lock. Ordinary configuration writers must remain excluded.
	if err := withLock(s.root, func() error { return nil }); !errors.Is(err, errLockBusy) {
		return errors.New("configuration writer was allowed during core restart")
	}
	startup, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := recoverConfig(startup, s.root); err != nil {
		return fmt.Errorf("service startup could not recover: %w", err)
	}
	return s.fakeServices.restart(ctx, name)
}

func TestCoreRestartAllowsStartupRecoveryAndExcludesWriters(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "update", true: "rollback"}[fail], func(t *testing.T) {
			root, candidate, u, _ := updaterFixture(t)
			services := &recoveringServices{fakeServices{root: root, fail: fail}}
			u.services = services
			u.validate = func(context.Context, string, string) error {
				if err := withFileLock(root, func() error { return nil }); !errors.Is(err, errLockBusy) {
					t.Fatal("core validation did not hold the configuration lock", err)
				}
				return nil
			}
			err := withOperationLock(root, func() error {
				_, err := u.activate(context.Background(), root, candidate, true)
				return err
			})
			expected, restarts := "new", 1
			if fail {
				expected, restarts = "old", 2
				if err == nil || !strings.Contains(err.Error(), "startup failed") {
					t.Fatal("new-core startup failure was not reported", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(fmt.Sprint(err), "could not recover") || len(services.calls) != restarts || string(readBytes(t, filepath.Join(root, "sing-box"))) != expected {
				t.Fatal("startup recovery blocked core update or rollback", services.calls, err)
			}
			if err := withLock(root, func() error { return nil }); err != nil {
				t.Fatal("core update did not release its operation lock", err)
			}
		})
	}
}

func TestCoreRollbackWaitsForConcurrentStartup(t *testing.T) {
	root, candidate, u, services := updaterFixture(t)
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	services.fail = true
	services.afterFailure = func() {
		go func() {
			done <- withFileLock(root, func() error {
				close(held)
				<-release
				return nil
			})
		}()
		select {
		case <-held:
		case <-time.After(time.Second):
			t.Fatal("concurrent startup did not hold the configuration lock")
		}
		time.AfterFunc(100*time.Millisecond, func() { close(release) })
	}
	err := withOperationLock(root, func() error {
		_, err := u.activate(context.Background(), root, candidate, true)
		return err
	})
	if err == nil || errors.Is(err, errLockBusy) || string(readBytes(t, filepath.Join(root, "sing-box"))) != "old" {
		t.Fatal("startup lock contention prevented automatic rollback", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type failedRecoveryServices struct {
	root  string
	err   error
	calls []string
}

func (s *failedRecoveryServices) active(context.Context) ([]string, error) {
	return []string{"sing-box", "sing-box-vpngate"}, nil
}

func (s *failedRecoveryServices) restart(ctx context.Context, name string) error {
	s.calls = append(s.calls, name)
	data, err := os.ReadFile(filepath.Join(s.root, "sing-box"))
	if err != nil {
		return err
	}
	if string(data) == "new" {
		return errors.New("new core failed to start")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.err
}

func TestCoreRollbackReportsServiceRecoveryFailure(t *testing.T) {
	root, candidate, u, _ := updaterFixture(t)
	recoveryErr := errors.New("original service failed to recover")
	services := &failedRecoveryServices{root: root, err: recoveryErr}
	u.services = services
	_, err := u.activate(context.Background(), root, candidate, true)
	if !errors.Is(err, recoveryErr) {
		t.Fatal("service recovery failure was hidden", err)
	}
	if string(readBytes(t, filepath.Join(root, "sing-box"))) != "old" || !reflect.DeepEqual(services.calls, []string{"sing-box", "sing-box", "sing-box-vpngate"}) {
		t.Fatal("rollback did not restore the original core or try both services", services.calls)
	}
}
