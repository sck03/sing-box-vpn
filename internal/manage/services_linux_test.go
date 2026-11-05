//go:build linux

package manage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLinuxServiceStartupRecovery(t *testing.T) {
	if os.Getenv("RUN_SERVICE_TESTS") != "1" {
		t.Skip("set RUN_SERVICE_TESTS=1 under Linux root with systemd")
	}
	if os.Geteuid() != 0 {
		t.Fatal("service recovery acceptance requires root")
	}
	manager := os.Getenv("GATEWAY_BINARY")
	if !filepath.IsAbs(manager) {
		t.Fatal("GATEWAY_BINARY must be an absolute path to the built manager")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := func(name string, args ...string) []byte {
		t.Helper()
		output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, output)
		}
		return output
	}
	name := "sb-recovery-" + strconv.Itoa(os.Getpid()) + "-" + randomUUID()[:8]
	namespace := name
	run("ip", "netns", "add", namespace)
	t.Cleanup(func() { _ = exec.Command("ip", "netns", "del", namespace).Run() })
	root := t.TempDir()
	app := App{Version: "recovered", Inbounds: []Object{{"type": "vmess", "listen_port": 20001}, {"type": "hysteria2", "listen_port": 20002}}, Profile: Profile{Ports: map[string]int{"hysteria2": 20002}, Hopping: []string{"31000:31002"}}}
	before, err := JSONBytes(app)
	if err != nil {
		t.Fatal(err)
	}
	interrupt := func() {
		t.Helper()
		if err := AtomicBytes(filepath.Join(root, "app.json"), []byte("interrupted write"), 0600); err != nil {
			t.Fatal(err)
		}
		writePendingJournal(t, root, fileBundle{"app.json": before}, snapshotStage)
	}
	interrupt()
	// Run only the firewall operation inside an isolated namespace; service
	// startup still exercises the real manager, recovery lock and systemd sandbox.
	wrapper := filepath.Join(root, "startup-manager")
	quotedManager := "'" + strings.ReplaceAll(manager, "'", "'\"'\"'") + "'"
	source := fmt.Sprintf("#!/bin/sh\nexec ip netns exec %s %s \"$@\"\n", namespace, quotedManager)
	if err := os.WriteFile(wrapper, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	unitDirectory := "/run/systemd/system"
	unitPath := filepath.Join(unitDirectory, name+".service")
	unit := fmt.Sprintf("[Unit]\nStartLimitIntervalSec=1h\nStartLimitBurst=1\n[Service]\nExecStart=%s -test.run=^TestServiceRecoveryHelper$\nEnvironment=SBVPN_SERVICE_HELPER=1 SBVPN_SERVICE_ROOT=%s\nRestart=no\nProtectSystem=strict\nReadOnlyPaths=%s\nNoNewPrivileges=true\n", strconv.Quote(binary), root, root)
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("systemctl", "stop", name+".service").Run()
		_ = exec.Command("systemctl", "reset-failed", name+".service").Run()
		_ = os.Remove(unitPath)
		directory := unitPath + ".d"
		_ = os.Remove(filepath.Join(directory, "10-sb-vpn-recovery.conf"))
		_ = os.Remove(directory)
		_ = exec.Command("systemctl", "daemon-reload").Run()
	})
	serviceScript(t, unitDirectory, root, wrapper, "write_service_recovery \"$SB_ROOT\" "+name)
	run("systemctl", "daemon-reload")
	run("systemctl", "start", name+".service")
	execute := func(c context.Context, binary string, args ...string) ([]byte, error) {
		return exec.CommandContext(c, "ip", append([]string{"netns", "exec", namespace, binary}, args...)...).CombinedOutput()
	}
	f := firewall{execute: execute}
	check := func() {
		t.Helper()
		if string(readBytes(t, filepath.Join(root, "app.json"))) != string(before) {
			t.Fatal("service started before recovering configuration")
		}
		if _, err := os.Stat(filepath.Join(root, journalName)); !os.IsNotExist(err) {
			t.Fatal("completed startup retained its recovery journal", err)
		}
		for _, binary := range []string{"iptables", "ip6tables"} {
			for _, chain := range []struct{ table, name string }{{"filter", inputChain}, {"nat", udpChain}} {
				if output, err := execute(ctx, binary, "-t", chain.table, "-S", chain.name); err != nil {
					t.Fatalf("startup did not restore %s %s: %v: %s", binary, chain.name, err, output)
				}
			}
		}
	}
	pid := func() string {
		return strings.TrimSpace(string(run("systemctl", "show", name+".service", "--property=MainPID", "--value")))
	}
	previous := pid()
	if previous == "0" || previous == "" {
		t.Fatal("startup helper was not running")
	}
	check()
	// Kernel rules are volatile across reboot, but the ownership marker and
	// recovery journal survive. Recreate this state before an unexpected exit.
	for _, binary := range []string{"iptables", "ip6tables"} {
		if err := f.removeChain(ctx, binary, "filter", "INPUT", inputChain); err != nil {
			t.Fatal(err)
		}
		if err := f.removeChain(ctx, binary, "nat", "PREROUTING", udpChain); err != nil {
			t.Fatal(err)
		}
	}
	interrupt()
	run("systemctl", "kill", "--signal=SIGTERM", "--kill-whom=main", name+".service")
	deadline := time.Now().Add(10 * time.Second)
	restarted := false
	for time.Now().Before(deadline) {
		current := pid()
		if current != "0" && current != previous {
			restarted = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !restarted {
		t.Log(string(run("journalctl", "-u", name+".service", "-n", "30", "--no-pager")))
		t.Fatal("unexpected clean exit did not restart despite the original start limit")
	}
	check()
	// Explicit stop remains stopped even with Restart=always.
	run("systemctl", "stop", name+".service")
	time.Sleep(3500 * time.Millisecond)
	if current := pid(); current != "0" {
		t.Fatal("explicitly stopped service restarted", current)
	}
}

func TestServiceRecoveryHelper(t *testing.T) {
	if os.Getenv("SBVPN_SERVICE_HELPER") != "1" {
		t.Skip("helper runs only in the isolated service acceptance test")
	}
	var app App
	if found, err := ReadJSON(filepath.Join(os.Getenv("SBVPN_SERVICE_ROOT"), "app.json"), &app); err != nil || !found || app.Version != "recovered" {
		t.Fatal("incomplete state reached the service", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer cancel()
	<-ctx.Done() // Return successfully, exercising Restart=always rather than an error exit.
}
