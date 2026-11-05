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
	"slices"
	"strconv"
	"strings"
	"time"
)

type firewall struct {
	execute func(context.Context, string, ...string) ([]byte, error)
	restore func(context.Context, string, string) ([]byte, error)
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
	if err == nil {
		return true, nil
	}
	var exit interface{ ExitCode() int }
	if !required && child.Err() == nil && errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("防火墙操作失败: %s: %w: %s", binary, errors.Join(err, child.Err()), strings.TrimSpace(string(output)))
}

const inputChain = "SBVPN_INPUT"
const udpChain = "SBVPN_UDP"
const legacyUDPChain = "SBVPN_HY2"

func (f firewall) apply(ctx context.Context, binary, rules string) error {
	child, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	binary += "-restore"
	var output []byte
	var err error
	if f.restore != nil {
		output, err = f.restore(child, binary, rules)
	} else {
		command := exec.CommandContext(child, binary, "-w", "5", "--noflush")
		command.Stdin = strings.NewReader(rules)
		output, err = command.CombinedOutput()
	}
	if err != nil {
		return fmt.Errorf("防火墙批量提交失败: %s: %w: %s", binary, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (f firewall) removeChain(ctx context.Context, binary, table, parent, chain string) error {
	found, err := f.command(ctx, false, binary, "-t", table, "-S", chain)
	if err != nil || !found {
		return err
	}
	return f.removeKnownChain(ctx, binary, table, parent, chain)
}

func (f firewall) removeKnownChain(ctx context.Context, binary, table, parent, chain string) error {
	prefix := []string{"-t", table}
	run := func(args ...string) error {
		_, err := f.command(ctx, true, binary, append(prefix, args...)...)
		return err
	}
	for {
		found, err := f.command(ctx, false, binary, append(prefix, "-C", parent, "-j", chain)...)
		if err != nil {
			return err
		}
		if !found {
			break
		}
		if err := run("-D", parent, "-j", chain); err != nil {
			return err
		}
	}
	if err := run("-F", chain); err != nil {
		return err
	}
	return run("-X", chain)
}

func firewallRules(app App, settings VPNSettings, gateway Object, subscription subscriptionSettings) (string, string, error) {
	reservations, err := portReservations(app, settings)
	if err != nil {
		return "", "", err
	}
	if subscription.Enabled {
		reservations = append(reservations, portReservation{portRange{subscription.Port, subscription.Port}, "", "subscription"})
	}
	inputs := append(append([]Object{}, app.Inbounds...), objects(gateway["inbounds"])...)
	ports := map[string]bool{}
	for _, in := range inputs {
		port, kind := number(in["listen_port"]), text(in["type"])
		if port < 1 || port > 65535 || !slices.Contains(Protocols, kind) {
			return "", "", errors.New("入口协议或端口错误")
		}
		network := "tcp"
		if kind == "hysteria2" || kind == "tuic" {
			network = "udp"
		}
		ports[network+" --dport "+strconv.Itoa(port)] = true
	}
	if subscription.Enabled {
		if subscription.Port < 1 || subscription.Port > 65535 {
			return "", "", errors.New("订阅端口错误")
		}
		ports["tcp --dport "+strconv.Itoa(subscription.Port)] = true
	}
	names := make([]string, 0, len(ports))
	for name := range ports {
		names = append(names, name)
	}
	slices.Sort(names)
	var input, udp strings.Builder
	for _, name := range names {
		fmt.Fprintf(&input, "-A %s -p %s -j ACCEPT\n", inputChain, name)
	}
	profiles := []Profile{app.Profile}
	if settings.ActiveCountry != "" {
		profile, exists := settings.Profiles[settings.ActiveCountry]
		if !exists {
			return "", "", errors.New("当前 VPNGate 国家缺少端口配置")
		}
		profiles = append(profiles, profile)
	}
	for i, profile := range profiles {
		entries := app.Inbounds
		if i > 0 {
			entries = objects(gateway["inbounds"])
		}
		for _, kind := range []string{"hysteria2", "tuic"} {
			values := forwardPorts(profile, kind)
			if len(values) == 0 {
				continue
			}
			if !slices.ContainsFunc(entries, func(in Object) bool {
				return in["type"] == kind && number(in["listen_port"]) == profile.Ports[kind]
			}) {
				return "", "", fmt.Errorf("%s 多端口的主端口与入口配置不一致", kind)
			}
			scope := normalScope
			if i > 0 {
				scope = vpngateScope
			}
			owner := normalScope
			if scope == vpngateScope {
				owner = profileOwner(settings.ActiveCountry)
			}
			ranges, err := parsePortRanges(values)
			if err != nil {
				return "", "", err
			}
			if err := validateReservedPorts(reservations, owner, kind, ranges); err != nil {
				return "", "", err
			}
			for _, r := range ranges {
				// Forward only packets addressed to this host, preserving transit traffic.
				fmt.Fprintf(&udp, "-A %s -p udp -m addrtype --dst-type LOCAL --dport %s -j REDIRECT --to-ports %d\n", udpChain, r.String(), profile.Ports[kind])
			}
		}
	}
	return input.String(), udp.String(), nil
}

type firewallPlan struct {
	binary, rules string
	legacy        bool
}

// Probe every required family before changing rules or claiming ownership.
func (f firewall) plan(ctx context.Context, binary, input, udp string, owned bool) (firewallPlan, error) {
	if binary == "ip6tables" {
		available, err := f.command(ctx, false, binary, "-S", "INPUT")
		if err != nil || !available {
			return firewallPlan{}, err
		}
	}
	inputHook, err := f.command(ctx, false, binary, "-C", "INPUT", "-j", inputChain)
	if err != nil {
		return firewallPlan{}, err
	}
	natHook, err := f.command(ctx, false, binary, "-t", "nat", "-C", "PREROUTING", "-j", udpChain)
	if err != nil {
		return firewallPlan{}, err
	}
	var rules strings.Builder
	fmt.Fprintf(&rules, "*filter\n:%s - [0:0]\n-F %s\n%s", inputChain, inputChain, input)
	if !inputHook {
		fmt.Fprintf(&rules, "-I INPUT 1 -j %s\n", inputChain)
	}
	rules.WriteString("COMMIT\n")
	if udp != "" {
		fmt.Fprintf(&rules, "*nat\n:%s - [0:0]\n-F %s\n%s", udpChain, udpChain, udp)
		if !natHook {
			fmt.Fprintf(&rules, "-I PREROUTING 1 -j %s\n", udpChain)
		}
		rules.WriteString("COMMIT\n")
	} else {
		exists := natHook
		if !exists {
			exists, err = f.command(ctx, false, binary, "-t", "nat", "-S", udpChain)
			if err != nil {
				return firewallPlan{}, err
			}
		}
		if exists {
			fmt.Fprintf(&rules, "*nat\n-F %s\n", udpChain)
			if natHook {
				fmt.Fprintf(&rules, "-D PREROUTING -j %s\n", udpChain)
			}
			fmt.Fprintf(&rules, "-X %s\nCOMMIT\n", udpChain)
		}
	}
	legacy := false
	if owned {
		legacy, err = f.command(ctx, false, binary, "-t", "nat", "-S", legacyUDPChain)
		if err != nil {
			return firewallPlan{}, err
		}
	}
	return firewallPlan{binary, rules.String(), legacy}, nil
}

func (f firewall) families() ([]string, error) {
	if runtime.GOOS == "linux" && f.execute == nil {
		if _, err := os.Stat("/proc/sys/net/ipv6"); errors.Is(err, os.ErrNotExist) {
			return []string{"iptables"}, nil
		} else if err != nil {
			return nil, err
		}
	}
	return []string{"iptables", "ip6tables"}, nil
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
	families, err := f.families()
	if err != nil {
		return err
	}
	if !owned {
		for _, binary := range families {
			for _, chain := range []struct{ table, name string }{{"filter", inputChain}, {"nat", udpChain}, {"nat", legacyUDPChain}} {
				found, err := f.command(ctx, false, binary, "-t", chain.table, "-S", chain.name)
				if err != nil {
					return err
				}
				if found {
					return fmt.Errorf("%s 已存在且无本项目所有权记录", chain.name)
				}
			}
		}
		if remove {
			return nil
		}
	}
	if remove {
		// Keep ownership until every query and owned-chain removal succeeds.
		for _, binary := range families {
			for _, chain := range []struct{ table, parent, name string }{{"filter", "INPUT", inputChain}, {"nat", "PREROUTING", udpChain}, {"nat", "PREROUTING", legacyUDPChain}} {
				if err := f.removeChain(ctx, binary, chain.table, chain.parent, chain.name); err != nil {
					return err
				}
			}
		}
		return os.Remove(marker)
	}
	var app App
	var settings VPNSettings
	var gateway Object
	var subscription subscriptionSettings
	for _, file := range []struct {
		name  string
		value any
	}{{"app.json", &app}, {"vpngate/settings.json", &settings}, {"vpngate/server.json", &gateway}, {"subscription.json", &subscription}} {
		if _, err := ReadJSON(filepath.Join(root, file.name), file.value); err != nil {
			return err
		}
	}
	input, udp, err := firewallRules(app, settings, gateway, subscription)
	if err != nil {
		return err
	}
	plans := make([]firewallPlan, 0, len(families))
	for _, binary := range families {
		plan, err := f.plan(ctx, binary, input, udp, owned)
		if err != nil {
			return err
		}
		if plan.binary != "" {
			plans = append(plans, plan)
		}
	}
	if !owned {
		if err := AtomicBytes(marker, []byte("sck03/sing-box-vpn\n"), 0600); err != nil {
			return err
		}
	}
	for _, plan := range plans {
		// --noflush preserves unrelated rules; each table changes at COMMIT.
		if err := f.apply(ctx, plan.binary, plan.rules); err != nil {
			return err
		}
		if plan.legacy {
			if err := f.removeKnownChain(ctx, plan.binary, "nat", "PREROUTING", legacyUDPChain); err != nil {
				return err
			}
		}
	}
	return nil
}

func RunFirewall(ctx context.Context, args []string, output io.Writer) error {
	f := flag.NewFlagSet("firewall", flag.ContinueOnError)
	f.SetOutput(output)
	root := f.String("root", "/etc/s-box", "configuration directory")
	remove := f.Bool("remove", false, "remove owned chains")
	wait := f.Bool("wait", false, "wait for configuration recovery before service startup")
	if err := f.Parse(args); err != nil {
		return err
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return errors.New("防火墙管理要求 Linux root")
	}
	if *wait {
		if *remove {
			return errors.New("--wait 仅用于启动时恢复防火墙")
		}
		return withStartupLock(ctx, *root, func() error { return (firewall{}).sync(ctx, *root, false) })
	}
	lock := withLock
	if *remove {
		// Confirmed uninstall must still work with a damaged recovery journal.
		lock = func(root string, operation func() error) error {
			return withOperationLock(root, func() error { return withFileLock(root, operation) })
		}
	}
	return lock(*root, func() error { return (firewall{}).sync(ctx, *root, *remove) })
}
