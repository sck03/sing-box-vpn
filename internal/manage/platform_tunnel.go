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
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"
)

var tunnelToken = regexp.MustCompile(`^[A-Za-z0-9+/=_-]+$`)

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
	unit := "# Managed by sing-box-vpn v4\n[Unit]\nDescription=sing-box-vpn Cloudflare tunnel\nAfter=network-online.target sb-core.service\nWants=network-online.target\n[Service]\nUMask=0077\nLogNamespace=sb-vpn\nRestart=on-failure\nRestartSec=10\nEnvironmentFile=" + envPath + "\n" + body + "[Install]\nWantedBy=multi-user.target\n"
	if old, e := os.ReadFile("/etc/systemd/system/sb-argo.service"); e == nil && !bytes.HasPrefix(old, []byte("# Managed by sing-box-vpn v4\n")) {
		return errors.New("refusing to overwrite an unrelated tunnel unit")
	}
	if err = AtomicBytes("/etc/systemd/system/sb-argo.service", []byte(unit), 0644); err != nil {
		return err
	}
	_, err = command.Output(ctx, nil, "systemctl", "daemon-reload")
	return err
}

func (s *PlatformService) syncTunnel(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	previous, err := s.Store.Observation(ctx, "argo-tunnel")
	if err != nil {
		return err
	}
	if c.Argo.Mode != "temporary" {
		if previous == "" {
			return nil
		}
		return s.Store.Observe(ctx, "argo-tunnel", "")
	}
	var next tunnelObservation
	if previous != "" {
		if err = json.Unmarshal([]byte(previous), &next); err != nil {
			return err
		}
	}
	if next.Settings != c.Argo {
		next = tunnelObservation{Settings: c.Argo}
	}
	next.Runtime, err = hostservice.ObserveTunnel(ctx, hostCommand, next.Runtime)
	if err != nil {
		return err
	}
	data, err := json.Marshal(next)
	if err != nil || string(data) == previous {
		return err
	}
	if err = s.Store.Observe(ctx, "argo-tunnel", string(data)); err != nil {
		return err
	}
	files, err := s.build(ctx, c)
	if err == nil {
		err = withStartupLock(ctx, s.Root, func() error { return publish(s.Root, files) })
	}
	if err != nil {
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, s.Store.Observe(recovery, "argo-tunnel", previous))
	}
	return err
}

func hostCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return command.Output(ctx, nil, name, args...)
}
