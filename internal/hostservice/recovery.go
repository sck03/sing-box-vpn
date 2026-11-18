// Package hostservice owns bounded host-service operations, independently of
// the management database so a storage outage cannot disable recovery itself.
package hostservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
)

type Command func(context.Context, string, ...string) ([]byte, error)

// Recover only retries failed project-owned units. Inactive units include
// operator stops and are never started by this controller.
func Recover(ctx context.Context, run Command, readFile func(string) ([]byte, error), units []string) error {
	var result error
	for _, name := range units {
		data, err := run(ctx, "systemctl", "show", name, "--property=LoadState,ActiveState,FragmentPath")
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		state := map[string]string{}
		for _, line := range strings.Split(string(data), "\n") {
			k, v, ok := strings.Cut(line, "=")
			if ok {
				state[k] = v
			}
		}
		if state["LoadState"] != "loaded" || state["ActiveState"] != "failed" || state["FragmentPath"] != "/etc/systemd/system/"+name+".service" {
			continue
		}
		unit, err := readFile(state["FragmentPath"])
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if !bytes.HasPrefix(unit, []byte(UnitOwner)) {
			continue
		}
		if _, err = run(ctx, "systemctl", "reset-failed", name); err == nil {
			_, err = run(ctx, "systemctl", "start", "--no-block", name)
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("recover %s: %w", name, err))
		}
	}
	return result
}
