package manage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func serviceScript(t *testing.T, directory, root, manager, script string) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required for service installation checks")
	}
	library, err := filepath.Abs("../../lib/services.sh")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, "-c", "set -euo pipefail; source \"$SERVICE_LIBRARY\"; "+script)
	cmd.Env = append(os.Environ(), "SERVICE_LIBRARY="+filepath.ToSlash(library), "SB_SYSTEMD_DIR="+filepath.ToSlash(directory), "SB_ROOT="+filepath.ToSlash(root), "CORE="+filepath.ToSlash(filepath.Join(root, "sing-box")), "SB_MANAGER="+filepath.ToSlash(manager))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("service installation: %v: %s", err, output)
	}
}

func TestServiceRecoveryIncludesExistingArgoAndPreservesOtherOverrides(t *testing.T) {
	directory, root := t.TempDir(), t.TempDir()
	legacy := "[Service]\nExecStart=/bin/true\nEnvironmentFile=/etc/s-box/argo.env\n"
	if err := os.WriteFile(filepath.Join(directory, "sb-argo.service"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	serviceScript(t, directory, root, "/bin/true", "write_main_service; write_subscription_service; write_gateway_services; write_service_recovery \"$SB_ROOT\" sb-argo missing-service")
	services := []string{"sing-box", "sb-subscription", "yg-vpngate", "sing-box-vpngate", "sb-argo"}
	for _, service := range services {
		path := filepath.Join(directory, service+".service.d", "10-sb-vpn-recovery.conf")
		data := string(readBytes(t, path))
		for _, expected := range []string{"StartLimitIntervalSec=0", "Restart=always", "RestartSec=3", `ExecStartPre=+/bin/true firewall --root "` + filepath.ToSlash(root) + `" --wait`} {
			if !strings.Contains(data, expected) {
				t.Fatalf("%s cannot recover automatically: missing %s", service, expected)
			}
		}
	}
	if string(readBytes(t, filepath.Join(directory, "sb-argo.service"))) != legacy {
		t.Fatal("recovery upgrade replaced the configured Argo command or token path")
	}
	if _, err := os.Stat(filepath.Join(directory, "missing-service.service.d")); !os.IsNotExist(err) {
		t.Fatal("recovery created an uninstalled service", err)
	}
	if data := string(readBytes(t, filepath.Join(directory, "sb-subscription.service"))); !strings.Contains(data, "ProtectSystem=strict") {
		t.Fatal("subscription lost its read-only sandbox")
	}
	gateway := string(readBytes(t, filepath.Join(directory, "sing-box-vpngate.service")))
	unit, _, _ := strings.Cut(gateway, "[Service]")
	if !strings.Contains(unit, "yg-vpngate.service") {
		t.Fatal("dedicated entries cannot start the required gateway daemon")
	}
	custom := filepath.Join(directory, "sing-box.service.d", "90-user.conf")
	if err := os.WriteFile(custom, []byte("user settings"), 0644); err != nil {
		t.Fatal(err)
	}
	serviceScript(t, directory, root, "/bin/true", "remove_service_recovery sing-box sb-subscription yg-vpngate sing-box-vpngate sb-argo")
	if string(readBytes(t, custom)) != "user settings" {
		t.Fatal("cleanup removed user service overrides")
	}
	for _, service := range services {
		if _, err := os.Stat(filepath.Join(directory, service+".service.d", "10-sb-vpn-recovery.conf")); !os.IsNotExist(err) {
			t.Fatal("owned recovery override remained after cleanup", service, err)
		}
	}
}
