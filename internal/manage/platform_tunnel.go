package manage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/command"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var tunnelToken = regexp.MustCompile(`^[A-Za-z0-9+/=_-]+$`)
var quickTunnelDomain = regexp.MustCompile(`https://([a-z0-9-]+\.trycloudflare\.com)`)
var tunnelInvocation = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (s *PlatformService) writeTunnel(ctx context.Context, c PlatformConfig) error {
	if c.Argo.Mode != "fixed" && c.Argo.Mode != "temporary" {
		return nil
	}
	binary := filepath.Join(s.Root, "cloudflared")
	if _, err := os.Stat(binary); errors.Is(err, os.ErrNotExist) {
		if err = RunAsset(ctx, []string{"--arch", runtime.GOARCH, "--output", binary}, io.Discard); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	quoted, err := unitQuote(binary)
	if err != nil {
		return err
	}
	envPath, err := unitQuote(filepath.Join(s.Root, "argo.env"))
	if err != nil {
		return err
	}
	body := ""
	env := ""
	if c.Argo.Mode == "fixed" {
		if !tunnelToken.MatchString(c.Argo.Token) {
			return errors.New("invalid tunnel token")
		}
		env = "TUNNEL_TOKEN=" + c.Argo.Token + "\n"
		body = "ExecStart=" + quoted + " tunnel --no-autoupdate run\n"
	} else {
		nodes, err := managedNodes(c.App)
		if err != nil {
			return err
		}
		port, scheme := 0, "http"
		for _, node := range nodes {
			if node.ID == c.Argo.Inbound {
				port = node.Port
				if node.TLS.Enabled {
					scheme = "https"
				}
			}
		}
		if port == 0 {
			return errors.New("tunnel inbound is missing")
		}
		body = fmt.Sprintf("ExecStart=%s tunnel --no-autoupdate --no-tls-verify --url %s://127.0.0.1:%d\n", quoted, scheme, port)
	}
	if err = AtomicBytes(filepath.Join(s.Root, "argo.env"), []byte(env), 0600); err != nil {
		return err
	}
	unit := "# Managed by sing-box-vpn v4\n[Unit]\nDescription=sing-box-vpn Cloudflare tunnel\nAfter=network-online.target sing-box.service\nWants=network-online.target\n[Service]\nUMask=0077\nRestart=on-failure\nRestartSec=10\nEnvironmentFile=" + envPath + "\n" + body + "[Install]\nWantedBy=multi-user.target\n"
	if old, e := os.ReadFile("/etc/systemd/system/sb-argo.service"); e == nil && !bytes.HasPrefix(old, []byte("# Managed by sing-box-vpn v4\n")) {
		return errors.New("refusing to overwrite an unrelated tunnel unit")
	}
	if err = AtomicBytes("/etc/systemd/system/sb-argo.service", []byte(unit), 0644); err != nil {
		return err
	}
	_, err = command.Output(ctx, nil, "systemctl", "daemon-reload")
	return err
}

// A temporary tunnel hostname is observed runtime state, not an editable secret
// or desired configuration. A bounded log tail is sufficient even after restart.
func (s *PlatformService) syncTunnel(ctx context.Context) error {
	state, err := s.Store.State(ctx)
	if err != nil {
		return err
	}
	if state.Revision != state.AppliedRevision {
		return nil
	}
	c, err := decodePlatform(state.Applied)
	if err != nil {
		return err
	}
	if c.Argo.Mode != "temporary" {
		return s.Store.Observe(ctx, "argo-domain", "")
	}
	invocation, err := command.Output(ctx, nil, "systemctl", "show", "sb-argo", "--property=InvocationID,ActiveState")
	if err != nil {
		return err
	}
	id, active := "", false
	for _, line := range strings.Split(string(invocation), "\n") {
		key, value, _ := strings.Cut(line, "=")
		if key == "InvocationID" {
			id = value
		}
		if key == "ActiveState" {
			active = value == "active"
		}
	}
	domain := ""
	if active && tunnelInvocation.MatchString(id) {
		data, e := command.Output(ctx, nil, "journalctl", "_SYSTEMD_INVOCATION_ID="+id, "-n", "200", "--no-pager", "-o", "cat")
		if e != nil {
			return e
		}
		if len(data) > 64<<10 {
			data = data[len(data)-(64<<10):]
		}
		matches := quickTunnelDomain.FindAllStringSubmatch(string(data), -1)
		if len(matches) > 0 {
			domain = strings.ToLower(matches[len(matches)-1][1])
		}
	}
	previous, err := s.Store.Observation(ctx, "argo-domain")
	if err != nil || previous == domain {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.Store.Observe(ctx, "argo-domain", domain); err != nil {
		return err
	}
	files, err := s.build(ctx, c)
	if err == nil {
		err = withStartupLock(ctx, s.Root, func() error { return publish(s.Root, files) })
	}
	if err != nil {
		_ = s.Store.Observe(context.Background(), "argo-domain", previous)
	}
	return err
}
