package manage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStartupWaitsForWriterAndRecoversBeforeAction(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	if err := withFileLock(root, func() error {
		if err := AtomicBytes(filepath.Join(root, "app.json"), []byte("incomplete"), 0600); err != nil {
			return err
		}
		writePendingJournal(t, root, fileBundle{"app.json": []byte("recovered")}, snapshotStage)
		go func() {
			done <- withStartupLock(ctx, root, func() error {
				data, err := os.ReadFile(filepath.Join(root, "app.json"))
				if err != nil {
					return err
				}
				if string(data) != "recovered" {
					return errors.New("startup observed incomplete configuration")
				}
				return nil
			})
		}()
		select {
		case err := <-done:
			t.Fatalf("startup did not wait for the writer: %v", err)
		case <-time.After(150 * time.Millisecond):
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("startup did not continue after recovery")
	}
}

func TestStartupLockCancellationDoesNotApplyFirewall(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := withStartupLock(ctx, root, func() error { called = true; return nil })
	if !errors.Is(err, context.Canceled) || called {
		t.Fatal("cancelled startup executed its action", err)
	}
}

func TestDamagedRecoveryPreventsStartupAction(t *testing.T) {
	root := t.TempDir()
	if err := AtomicBytes(filepath.Join(root, journalName), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := withStartupLock(context.Background(), root, func() error { called = true; return nil }); err == nil || called {
		t.Fatal("startup applied firewall despite a damaged recovery journal")
	}
}
