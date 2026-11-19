package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/command"
	"github.com/sck03/sing-box-vpn/internal/hostservice"
	"os"
	"slices"
	"strings"
)

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
