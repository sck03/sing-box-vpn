package accounting

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

// A private tmpfs bounds the failure injection; no host filesystem is filled.
func TestLinuxDiskFullRecovery(t *testing.T) {
	if os.Getenv("RUN_STORAGE_TESTS") != "1" {
		t.Skip("set RUN_STORAGE_TESTS=1 as root on a disposable Linux host")
	}
	root := t.TempDir()
	if err := syscall.Mount("tmpfs", root, "tmpfs", 0, "size=8m,mode=0700"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := syscall.Unmount(root, 0); err != nil {
			t.Error(err)
		}
	}()
	s, err := Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SaveAccount(t.Context(), domain.Account{ID: "a", Name: "disk-full"}); err != nil {
		t.Fatal(err)
	}
	filler := filepath.Join(root, "filler")
	f, err := os.Create(filler)
	if err != nil {
		t.Fatal(err)
	}
	block := make([]byte, 64<<10)
	for err == nil {
		_, err = f.Write(block)
	}
	f.Close()
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatal(err)
	}
	var accepted int64
	failed := false
	for i := 0; i < 2048; i++ {
		n, e := s.Reserve(t.Context(), fmt.Sprintf("lease-%d", i), "a", "normal", ReservationBytes, time.Now())
		if e != nil {
			if n != 0 {
				t.Fatal("failed reservation granted bytes", n)
			}
			failed = true
			break
		}
		accepted += n
	}
	if !failed {
		t.Fatal("disk exhaustion never rejected reservation")
	}
	if err = os.Remove(filler); err != nil {
		t.Fatal(err)
	}
	u, err := s.Account(t.Context(), "a")
	if err != nil || u.Charged != accepted {
		t.Fatal("partial failed transaction", u.Charged, accepted, err)
	}
	if _, err = s.Reserve(t.Context(), "recovered", "a", "normal", ReservationBytes, time.Now()); err != nil {
		t.Fatal(err)
	}
	var integrity string
	if err = s.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal(integrity, err)
	}
	t.Log("ENOSPC rejected uncommitted reservations; recovered after freeing space; integrity_check=ok")
}

func TestLinuxKilledMeterPreservesReservation(t *testing.T) {
	if root := os.Getenv("SBVPN_CRASH_CHILD"); root != "" {
		s, err := Open(filepath.Join(root, "control.db"))
		if err != nil {
			panic(err)
		}
		m := NewMeter(s, "normal")
		h, err := m.Open(t.Context(), "c.1", "in", &closedCounter{})
		if err != nil {
			panic(err)
		}
		n, err := h.Take(4096)
		if err != nil {
			panic(err)
		}
		h.Complete(n, n, true)
		if err = os.WriteFile(filepath.Join(root, "ready"), []byte("ready"), 0600); err != nil {
			panic(err)
		}
		select {}
	}
	if os.Getenv("RUN_STORAGE_TESTS") != "1" {
		t.Skip("set RUN_STORAGE_TESTS=1")
	}
	root := t.TempDir()
	s, err := Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SaveAccount(t.Context(), domain.Account{ID: "a", Name: "crash"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Bind(t.Context(), domain.Credential{ID: "c", AccountID: "a", RuntimeID: "normal", InboundID: "in", Protocol: "vless", Generation: 1, UUID: "test"}); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestLinuxKilledMeterPreservesReservation$")
	cmd.Env = append(os.Environ(), "SBVPN_CRASH_CHILD="+root)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err = os.Stat(filepath.Join(root, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child not ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	u, err := s.Account(t.Context(), "a")
	if err != nil || u.Charged != ReservationBytes || u.Upload != 0 {
		t.Fatal(u, err)
	}
	t.Log("SIGKILL preserved the full durable reservation without refunding uncheckpointed traffic")
}
