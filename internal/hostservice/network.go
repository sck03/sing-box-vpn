package hostservice

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
)

// RestoreNetwork is retryable after a partial sysctl failure, file removal or
// process interruption. The caller clears its durable recovery record last.
func RestoreNetwork(ctx context.Context, run Command, path string, values map[string]string) error {
	keys := []string{"net.core.default_qdisc", "net.ipv4.tcp_congestion_control"}
	for _, key := range keys {
		if values[key] == "" || strings.ContainsAny(values[key], "\r\n\x00") {
			return errors.New("invalid saved sysctl value")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && !bytes.HasPrefix(data, []byte("# sing-box-vpn\n")) {
		return errors.New("sysctl file is not owned by this project")
	}
	for _, key := range keys {
		if _, err = run(ctx, "sysctl", "-w", key+"="+values[key]); err != nil {
			return err
		}
	}
	if err = os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
