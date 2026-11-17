package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/command"
	"github.com/sck03/sing-box-vpn/internal/hostservice"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"time"
)

var platformServices = []string{"sing-box", "sing-box-vpngate", "yg-vpngate", "sb-subscription", "sb-argo"}

func scopeService(scope string) string {
	if scope == "vpngate" {
		return "sing-box-vpngate"
	}
	return "sing-box"
}
func (s *PlatformService) activeServices(ctx context.Context) ([]string, error) {
	result := []string{}
	for _, name := range platformServices {
		out, err := command.Output(ctx, nil, "systemctl", "show", name, "--property=ActiveState", "--value")
		if err != nil {
			return nil, fmt.Errorf("query %s: %w", name, err)
		}
		switch strings.TrimSpace(string(out)) {
		case "active":
			result = append(result, name)
		case "inactive", "failed":
		default:
			return nil, fmt.Errorf("%s is transitioning; retry after it settles", name)
		}
	}
	return result, nil
}
func (s *PlatformService) service(ctx context.Context, name, action string) error {
	if !slices.Contains(platformServices, name) || !slices.Contains([]string{"start", "stop", "restart", "try-restart", "enable", "disable"}, action) {
		return errors.New("unsupported service action")
	}
	started := time.Now().UnixNano()
	if action == "start" {
		if _, e := command.Output(ctx, nil, "systemctl", "is-active", "--quiet", name); e == nil {
			started = 0
		}
	}
	_, err := command.Output(ctx, nil, "systemctl", action, name)
	if err == nil && (action == "start" || action == "restart") {
		_, err = command.Output(ctx, nil, "systemctl", "is-active", "--quiet", name)
		if err == nil && (name == "sing-box" || name == "sing-box-vpngate") {
			instance := "normal"
			if name == "sing-box-vpngate" {
				instance = "vpngate"
			}
			err = s.awaitRuntime(ctx, instance, started)
		}
	}
	return err
}
func (s *PlatformService) ServiceStatus(ctx context.Context) ([]Object, error) {
	states := map[string]Object{}
	var queryErr error
	if !s.NoSystem {
		args := append([]string{"show", "--property=Id,ActiveState,SubState,UnitFileState,LoadState,MainPID"}, platformServices...)
		var data []byte
		data, queryErr = command.Output(ctx, nil, "systemctl", args...)
		for _, block := range strings.Split(strings.TrimSpace(string(data)), "\n\n") {
			v := Object{}
			for _, line := range strings.Split(block, "\n") {
				if k, value, ok := strings.Cut(line, "="); ok {
					v[k] = value
				}
			}
			states[strings.TrimSuffix(text(v["Id"]), ".service")] = v
		}
	}
	result := []Object{}
	for _, name := range platformServices {
		v := Object{"name": name}
		if s.NoSystem {
			v["state"] = "unavailable"
		} else if state, ok := states[name]; ok {
			for key, value := range state {
				v[key] = value
			}
		} else {
			v["error"] = "service query failed"
			if queryErr == nil {
				v["error"] = "service missing from systemctl response"
			}
		}
		result = append(result, v)
	}
	return result, nil
}
func (s *PlatformService) Logs(ctx context.Context, name string) (string, error) {
	if !slices.Contains(append(append([]string{}, platformServices...), "sb-panel"), name) {
		return "", errors.New("unknown service")
	}
	data, err := command.Output(ctx, nil, "journalctl", "-u", name, "-n", "120", "--no-pager", "-o", "short-iso")
	if len(data) > 128<<10 {
		data = data[len(data)-(128<<10):]
	}
	// Core/gateway logs are diagnostic and may contain user-selected targets.
	// Never stream process command lines or environment files as a log source.
	return string(data), err
}

type serviceChange struct{ Name, Action string }

func (s *PlatformService) enabledServices(ctx context.Context) ([]string, error) {
	result := []string{}
	for _, name := range platformServices {
		out, err := command.Output(ctx, nil, "systemctl", "show", name, "--property=UnitFileState", "--value")
		if err != nil {
			return nil, err
		}
		switch strings.TrimSpace(string(out)) {
		case "enabled":
			result = append(result, name)
		case "disabled", "", "static":
		default:
			return nil, fmt.Errorf("%s has an unmanaged enable state", name)
		}
	}
	return result, nil
}
func (s *PlatformService) restoreEnabled(ctx context.Context, enabled []string) error {
	var result error
	for _, name := range platformServices {
		action := "disable"
		if slices.Contains(enabled, name) {
			action = "enable"
		}
		// A disabled tunnel may have no unit yet.
		if _, err := os.Stat(filepath.Join("/etc/systemd/system", name+".service")); errors.Is(err, os.ErrNotExist) {
			continue
		}
		result = errors.Join(result, s.service(ctx, name, action))
	}
	return result
}

// Only a newly enabled component is started automatically. Editing a component
// that an operator stopped preserves that stopped state.
func deploymentPlan(c, old PlatformConfig, active []string) []serviceChange {
	currentServer, _ := serverForApp(c.App)
	oldServer, _ := serverForApp(old.App)
	vpnChanged := !reflect.DeepEqual(c.VPN, old.VPN) || !reflect.DeepEqual(c.VPNSecrets, old.VPNSecrets)
	changed := map[string]bool{
		"sing-box":         !reflect.DeepEqual(currentServer, oldServer),
		"yg-vpngate":       !reflect.DeepEqual(c.Gateway, old.Gateway),
		"sing-box-vpngate": vpnChanged || !reflect.DeepEqual(c.Gateway, old.Gateway),
		"sb-subscription":  c.Listener != old.Listener,
		"sb-argo":          c.Argo != old.Argo || !reflect.DeepEqual(currentServer, oldServer),
	}
	enabled := func(v PlatformConfig, name string) bool {
		switch name {
		case "yg-vpngate", "sing-box-vpngate":
			return v.VPN.ActiveCountry != "" && v.Gateway["enabled"] != false
		case "sb-subscription":
			return v.Listener.Enabled
		case "sb-argo":
			return v.Argo.Mode == "fixed" || v.Argo.Mode == "temporary"
		default:
			return true
		}
	}
	plan := []serviceChange{}
	for _, name := range []string{"sing-box-vpngate", "yg-vpngate", "sb-subscription", "sb-argo"} {
		if !enabled(c, name) && enabled(old, name) {
			plan = append(plan, serviceChange{name, "disable-stop"})
		}
	}
	for _, name := range []string{"yg-vpngate", "sing-box", "sing-box-vpngate", "sb-subscription", "sb-argo"} {
		if !enabled(c, name) {
			continue
		}
		if !enabled(old, name) {
			plan = append(plan, serviceChange{name, "enable-start"})
		} else if changed[name] && slices.Contains(active, name) {
			plan = append(plan, serviceChange{name, "restart"})
		}
	}
	return plan
}

func (s *PlatformService) applyHost(ctx context.Context, c, old PlatformConfig, active []string, rollback bool) error {
	if runtime.GOOS != "linux" {
		return errors.New("deployment requires Linux systemd")
	}
	if err := s.WriteUnits(ctx); err != nil {
		return err
	}
	if err := s.writeTunnel(ctx, c); err != nil {
		return err
	}
	if err := withStartupLock(ctx, s.Root, func() error { return (firewall{}).sync(ctx, s.Root, false) }); err != nil {
		return err
	}
	plan := deploymentPlan(c, old, active)
	if rollback {
		plan = nil
		for _, name := range []string{"sing-box-vpngate", "yg-vpngate", "sb-subscription", "sb-argo", "sing-box"} {
			if !slices.Contains(active, name) {
				load, err := command.Output(ctx, nil, "systemctl", "show", name, "--property=LoadState", "--value")
				if err != nil {
					return err
				}
				if strings.TrimSpace(string(load)) == "not-found" {
					continue
				}
				plan = append(plan, serviceChange{name, "stop"})
			}
		}
		for _, name := range []string{"yg-vpngate", "sing-box", "sing-box-vpngate", "sb-subscription", "sb-argo"} {
			if slices.Contains(active, name) {
				plan = append(plan, serviceChange{name, "restart"})
			}
		}
	}
	for _, item := range plan {
		action := item.Action
		if action == "enable-start" || action == "disable-stop" {
			setting, operation := "enable", "start"
			if action == "disable-stop" {
				setting, operation = "disable", "stop"
			}
			if err := s.service(ctx, item.Name, setting); err != nil {
				return err
			}
			action = operation
		}
		if err := s.service(ctx, item.Name, action); err != nil {
			return err
		}
	}
	return nil
}

func (s *PlatformService) awaitRuntime(ctx context.Context, instance string, started int64) error {
	child, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		states, err := s.Store.Runtimes(child)
		if err != nil {
			return err
		}
		for _, state := range states {
			if state["instance"] == instance && state["started"].(int64) >= started && state["recent"] == true {
				return nil
			}
		}
		select {
		case <-child.Done():
			return fmt.Errorf("%s did not confirm startup: %w", instance, child.Err())
		case <-ticker.C:
		}
	}
}

func unitQuote(v string) (string, error) {
	if strings.ContainsAny(v, "\r\n\x00\"%$") {
		return "", errors.New("systemd path contains unsupported characters")
	}
	return `"` + v + `"`, nil
}
func (s *PlatformService) WriteUnits(ctx context.Context) error {
	root, err := unitQuote(s.Root)
	if err != nil {
		return err
	}
	binary, err := unitQuote(s.Runtime)
	if err != nil {
		return err
	}
	db, err := unitQuote(filepath.Join(s.Root, "control.db"))
	if err != nil {
		return err
	}
	manager := "/usr/local/bin/yg-vpngate"
	config, err := unitQuote(filepath.Join(s.Root, "sb.json"))
	if err != nil {
		return err
	}
	vpnConfig, err := unitQuote(filepath.Join(s.Root, "vpngate", "server.json"))
	if err != nil {
		return err
	}
	gwConfig, err := unitQuote(filepath.Join(s.Root, "vpngate", "gateway.json"))
	if err != nil {
		return err
	}
	core, err := unitQuote(s.Core)
	if err != nil {
		return err
	}
	units := map[string]string{
		"sing-box":         fmt.Sprintf("ExecStart=%s --config %s --database %s --instance normal\n", binary, config, db),
		"sing-box-vpngate": fmt.Sprintf("ExecStart=%s --config %s --database %s --instance vpngate\n", binary, vpnConfig, db),
		"yg-vpngate":       fmt.Sprintf("ExecStart=%s daemon --config %s\nExecStopPost=%s cleanup --config %s\n", manager, gwConfig, manager, gwConfig),
		"sb-subscription":  fmt.Sprintf("ExecStart=%s subscription --root %s\n", manager, root),
		"sb-panel":         fmt.Sprintf("ExecStart=%s panel --root %s --core %s --runtime %s serve\n", manager, root, core, binary),
	}
	for name, body := range units {
		extra := ""
		if name == "sing-box-vpngate" {
			extra = "Requires=yg-vpngate.service\nAfter=yg-vpngate.service\n"
		}
		pre := ""
		if name == "sing-box" || name == "sing-box-vpngate" {
			pre = fmt.Sprintf("ExecStartPre=%s firewall --root %s --wait\n", manager, root)
		}
		// WorkingDirectory is a single path, unlike the quoted ExecStart argv.
		data := "# Managed by sing-box-vpn v4\n[Unit]\nDescription=sing-box-vpn " + name + "\nAfter=network-online.target\nWants=network-online.target\nStartLimitIntervalSec=300\nStartLimitBurst=10\n" + extra + "[Service]\nUMask=0077\nRestart=always\nRestartSec=15\nTimeoutStopSec=30\nLimitNOFILE=65536\n" + pre + body + "WorkingDirectory=" + s.Root + "\n[Install]\nWantedBy=multi-user.target\n"
		path := filepath.Join("/etc/systemd/system", name+".service")
		if old, e := os.ReadFile(path); e == nil && !bytes.Contains(old, []byte("sing-box-vpn")) && !bytes.Contains(old, []byte("VPNGate")) {
			return fmt.Errorf("refusing to overwrite unrelated unit %s", name)
		}
		if err = AtomicBytes(path, []byte(data), 0644); err != nil {
			return err
		}
	}
	for name, data := range recoveryUnits(root) {
		path := filepath.Join("/etc/systemd/system", name)
		if old, e := os.ReadFile(path); e == nil && !bytes.Contains(old, []byte("sing-box-vpn")) {
			return fmt.Errorf("refusing to overwrite unrelated unit %s", name)
		}
		if err = AtomicBytes(path, []byte(data), 0644); err != nil {
			return err
		}
	}
	if _, err = command.Output(ctx, nil, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	_, err = command.Output(ctx, nil, "systemctl", "enable", "--now", "sb-recovery.timer")
	return err
}

func (s *PlatformService) gateway(ctx context.Context, resource string, payload []byte) (json.RawMessage, error) {
	if resource != "status" && resource != "nodes" && resource != "actions" {
		return nil, errors.New("unknown gateway resource")
	}
	if resource == "actions" {
		var v struct {
			Op string `json:"op"`
		}
		if err := json.Unmarshal(payload, &v); err != nil {
			return nil, err
		}
		if !slices.Contains([]string{"start", "stop", "swap", "refresh"}, v.Op) {
			return nil, errors.New("edit persistent gateway policy in configuration")
		}
	}
	state, err := s.Store.State(ctx)
	if err != nil {
		return nil, err
	}
	c, err := decodePlatform(state.Applied)
	if err != nil {
		return nil, err
	}
	run := text(c.Gateway["run_dir"])
	if run == "" {
		run = "/run/yg-vpngate"
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", filepath.Join(run, "control.sock"))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 90 * time.Second}
	method := "GET"
	if resource == "actions" {
		method = "POST"
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://gateway/v1/"+resource, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("VPNGate control unavailable; inspect service status")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("gateway operation failed (HTTP %d)", res.StatusCode)
	}
	return data, nil
}

func (s *PlatformService) setBBR(ctx context.Context, mode string) error {
	if s.NoSystem {
		return errors.New("system operations are disabled")
	}
	path := "/etc/sysctl.d/90-sing-box-vpn.conf"
	if mode == "restore" {
		previous, err := s.Store.Observation(ctx, "bbr-previous")
		if err != nil {
			return err
		}
		var values map[string]string
		if err = json.Unmarshal([]byte(previous), &values); err != nil {
			return errors.New("original BBR settings are unavailable")
		}
		if err = hostservice.RestoreNetwork(ctx, hostCommand, path, values); err != nil {
			return err
		}
		return s.Store.Observe(ctx, "bbr-previous", "")
	}
	if mode != "enable" {
		return errors.New("BBR operation must be enable or restore")
	}
	data, err := command.Output(ctx, nil, "sysctl", "-n", "net.ipv4.tcp_available_congestion_control")
	if err != nil {
		return err
	}
	if !slices.Contains(strings.Fields(string(data)), "bbr") {
		return errors.New("kernel does not advertise BBR")
	}
	if old, e := os.ReadFile(path); e == nil && !bytes.HasPrefix(old, []byte("# sing-box-vpn\n")) {
		return errors.New("refusing to replace unrelated sysctl file")
	}
	if saved, e := s.Store.Observation(ctx, "bbr-previous"); e != nil {
		return e
	} else if saved == "" {
		values, e := s.NetworkSettings(ctx)
		if e != nil {
			return e
		}
		raw, _ := json.Marshal(values)
		if e = s.Store.Observe(ctx, "bbr-previous", string(raw)); e != nil {
			return e
		}
	}
	if err = AtomicBytes(path, []byte("# sing-box-vpn\nnet.core.default_qdisc=fq\nnet.ipv4.tcp_congestion_control=bbr\n"), 0644); err != nil {
		return err
	}
	_, err = command.Output(ctx, nil, "sysctl", "-p", path)
	return err
}

func (s *PlatformService) NetworkSettings(ctx context.Context) (map[string]string, error) {
	if s.NoSystem {
		return nil, errors.New("system operations are disabled")
	}
	result := map[string]string{}
	for _, key := range []string{"net.core.default_qdisc", "net.ipv4.tcp_congestion_control", "net.ipv4.tcp_available_congestion_control"} {
		v, err := command.Output(ctx, nil, "sysctl", "-n", key)
		if err != nil {
			return nil, err
		}
		result[key] = strings.TrimSpace(string(v))
	}
	return result, nil
}
