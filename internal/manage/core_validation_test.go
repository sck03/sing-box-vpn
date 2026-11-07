package manage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCoreValidationRejectsNewConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sbox.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	checks := 0
	err := validateConfigs(context.Background(), root, "candidate", func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		checks++
		return nil, os.WriteFile(filepath.Join(root, "sb.json"), []byte(`{"new":"unchecked"}`), 0600)
	})
	if checks != 1 || err == nil {
		t.Fatalf("new unchecked server configuration accepted: checks=%d, err=%v", checks, err)
	}
}

func TestCoreValidationSnapshotsAllPathsBeforeChecking(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sb.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	checks := 0
	err := validateConfigs(context.Background(), root, "candidate", func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		checks++
		return nil, AtomicBytes(filepath.Join(root, "vpngate", "sbox.json"), []byte(`{}`), 0600)
	})
	if checks != 1 || err == nil {
		t.Fatalf("configuration set changed during validation: checks=%d, err=%v", checks, err)
	}
}

func TestCoreValidationRejectsRemovedConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sb.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	err := validateConfigs(context.Background(), root, "candidate", func(context.Context, string, ...string) ([]byte, error) {
		return nil, os.Remove(path)
	})
	if err == nil {
		t.Fatal("removed configuration accepted")
	}
}

func TestCoreValidationRejectsNonRegularConfigBeforeCheck(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sb.json"), 0700); err != nil {
		t.Fatal(err)
	}
	err := validateConfigs(context.Background(), root, "candidate", func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("core checked a non-regular file")
		return nil, nil
	})
	if err == nil {
		t.Fatal("non-regular configuration accepted")
	}
}

func TestCoreValidationCancellation(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := validateConfigs(ctx, root, "candidate", func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("core started after cancellation")
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

type cancelActiveServices struct {
	cancel context.CancelFunc
	fakeServices
}

func (s *cancelActiveServices) active(context.Context) ([]string, error) {
	s.cancel()
	return nil, nil
}

func TestCoreCancellationBeforeActivationKeepsCurrent(t *testing.T) {
	root, candidate, updater, _ := updaterFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updater.services = &cancelActiveServices{cancel: cancel}
	previous := filepath.Join(root, "sing-box.previous")
	if err := os.WriteFile(previous, []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := updater.activate(ctx, root, candidate, true)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("canceled activation returned %v", err)
	}
	if string(readBytes(t, filepath.Join(root, "sing-box"))) != "old" || string(readBytes(t, previous)) != "previous" {
		t.Fatal("canceled update replaced current or previous core")
	}
}

func BenchmarkCoreConfigValidation(b *testing.B) {
	root := b.TempDir()
	data := append(bytes.Repeat([]byte(" "), (4<<20)-2), '{', '}')
	if err := os.WriteFile(filepath.Join(root, "sb.json"), data, 0600); err != nil {
		b.Fatal(err)
	}
	check := func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	b.ReportAllocs()
	b.SetBytes(int64(2 * len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := validateConfigs(context.Background(), root, "candidate", check); err != nil {
			b.Fatal(err)
		}
	}
}
