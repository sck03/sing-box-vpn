package manage

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type firewall struct {
	execute func(context.Context, string, ...string) ([]byte, error)
}

func (f firewall) command(ctx context.Context, required bool, binary string, args ...string) (bool, error) {
	child, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	full := append([]string{"-w", "5"}, args...)
	var output []byte
	var err error
	if f.execute != nil {
		output, err = f.execute(child, binary, full...)
	} else {
		output, err = exec.CommandContext(child, binary, full...).CombinedOutput()
	}
	if err != nil && required {
		return false, fmt.Errorf("防火墙操作失败: %s: %w: %s", binary, err, strings.TrimSpace(string(output)))
	}
	return err == nil, nil
}

func (f firewall) sync(ctx context.Context, root string, remove bool) error {
	marker := filepath.Join(root, "firewall.owner")
	owner, err := os.ReadFile(marker)
	owned := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if owned && strings.TrimSpace(string(owner)) != "sck03/sing-box-vpn" {
		return errors.New("防火墙所有权记录不匹配")
	}
	run := func(binary string, args ...string) error { _, err := f.command(ctx, true, binary, args...); return err }
	has := func(binary string, args ...string) bool { ok, _ := f.command(ctx, false, binary, args...); return ok }
	if !owned {
		for _, binary := range []string{"iptables", "ip6tables"} {
			if has(binary, "-S", "SBVPN_INPUT") {
				return errors.New("SBVPN_INPUT 已存在且无本项目所有权记录")
			}
		}
		if has("iptables", "-t", "nat", "-S", "SBVPN_HY2") {
			return errors.New("SBVPN_HY2 已存在且无本项目所有权记录")
		}
		if remove {
			return nil
		}
		if err := AtomicBytes(marker, []byte("sck03/sing-box-vpn\n"), 0600); err != nil {
			return err
		}
	}
	var app App
	if _, err = ReadJSON(filepath.Join(root, "app.json"), &app); err != nil {
		return err
	}
	var gateway Object
	if _, err = ReadJSON(filepath.Join(root, "vpngate", "server.json"), &gateway); err != nil {
		return err
	}
	var subscription subscriptionSettings
	if _, err = ReadJSON(filepath.Join(root, "subscription.json"), &subscription); err != nil {
		return err
	}
	inputs := append(append([]Object{}, app.Inbounds...), objects(gateway["inbounds"])...)
	for _, binary := range []string{"iptables", "ip6tables"} {
		if remove {
			for has(binary, "-C", "INPUT", "-j", "SBVPN_INPUT") {
				if err := run(binary, "-D", "INPUT", "-j", "SBVPN_INPUT"); err != nil {
					return err
				}
			}
			_, _ = f.command(ctx, false, binary, "-F", "SBVPN_INPUT")
			_, _ = f.command(ctx, false, binary, "-X", "SBVPN_INPUT")
			continue
		}
		if !has(binary, "-S", "SBVPN_INPUT") {
			if !has(binary, "-N", "SBVPN_INPUT") {
				if binary == "ip6tables" {
					continue
				}
				return errors.New("无法创建 IPv4 防火墙链")
			}
		}
		if err := run(binary, "-F", "SBVPN_INPUT"); err != nil {
			return err
		}
		for _, in := range inputs {
			port := number(in["listen_port"])
			if port < 1 || port > 65535 {
				return errors.New("入口端口错误")
			}
			protocol := "tcp"
			if in["type"] == "hysteria2" || in["type"] == "tuic" {
				protocol = "udp"
			}
			if err := run(binary, "-A", "SBVPN_INPUT", "-p", protocol, "--dport", strconv.Itoa(port), "-j", "ACCEPT"); err != nil {
				return err
			}
		}
		if subscription.Enabled {
			if subscription.Port < 1 || subscription.Port > 65535 {
				return errors.New("订阅端口错误")
			}
			if err := run(binary, "-A", "SBVPN_INPUT", "-p", "tcp", "--dport", strconv.Itoa(subscription.Port), "-j", "ACCEPT"); err != nil {
				return err
			}
		}
		for _, port := range app.Profile.Hopping {
			if err := run(binary, "-A", "SBVPN_INPUT", "-p", "udp", "--dport", port, "-j", "ACCEPT"); err != nil {
				return err
			}
		}
		if !has(binary, "-C", "INPUT", "-j", "SBVPN_INPUT") {
			if err := run(binary, "-I", "INPUT", "1", "-j", "SBVPN_INPUT"); err != nil {
				return err
			}
		}
	}
	if owned {
		for has("iptables", "-t", "nat", "-C", "PREROUTING", "-j", "SBVPN_HY2") {
			if err := run("iptables", "-t", "nat", "-D", "PREROUTING", "-j", "SBVPN_HY2"); err != nil {
				return err
			}
		}
		_, _ = f.command(ctx, false, "iptables", "-t", "nat", "-F", "SBVPN_HY2")
		_, _ = f.command(ctx, false, "iptables", "-t", "nat", "-X", "SBVPN_HY2")
	}
	if remove {
		return os.Remove(marker)
	}
	if len(app.Profile.Hopping) > 0 {
		if err := run("iptables", "-t", "nat", "-N", "SBVPN_HY2"); err != nil {
			return err
		}
		port := app.Profile.Ports["hysteria2"]
		for _, value := range app.Profile.Hopping {
			if err := run("iptables", "-t", "nat", "-A", "SBVPN_HY2", "-p", "udp", "--dport", value, "-j", "REDIRECT", "--to-ports", strconv.Itoa(port)); err != nil {
				return err
			}
		}
		if err := run("iptables", "-t", "nat", "-I", "PREROUTING", "1", "-j", "SBVPN_HY2"); err != nil {
			return err
		}
	}
	return nil
}

func RunFirewall(ctx context.Context, args []string, output io.Writer) error {
	f := flag.NewFlagSet("firewall", flag.ContinueOnError)
	f.SetOutput(output)
	root := f.String("root", "/etc/s-box", "configuration directory")
	remove := f.Bool("remove", false, "remove owned chains")
	if err := f.Parse(args); err != nil {
		return err
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return errors.New("防火墙管理要求 Linux root")
	}
	return withLock(*root, func() error { return (firewall{}).sync(ctx, *root, *remove) })
}
