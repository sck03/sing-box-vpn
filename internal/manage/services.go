package manage

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/sck03/sing-box-vpn/internal/command"
)

type serviceManager interface {
	active(context.Context) ([]string, error)
	restart(context.Context, string) error
}

type systemServices struct{}

func (systemServices) active(ctx context.Context) ([]string, error) {
	var active []string
	for _, name := range []string{"sing-box", "sing-box-vpngate"} {
		child, cancel := context.WithTimeout(ctx, 10*time.Second)
		output, err := command.Output(child, nil, "systemctl", "is-active", "--quiet", name)
		contextErr := child.Err()
		cancel()
		if contextErr != nil {
			return nil, fmt.Errorf("查询 %s 服务状态失败: %w", name, contextErr)
		}
		if err == nil {
			active = append(active, name)
			continue
		}
		// --quiet returns no output for inactive/missing units (LSB 3/4).
		// Bus/permission failures, cancellation and incomplete output are errors,
		// never evidence that a service can be omitted from the restart plan.
		var exit *exec.ExitError
		if len(output) == 0 && errors.As(err, &exit) && (exit.ExitCode() == 3 || exit.ExitCode() == 4) {
			continue
		}
		return nil, fmt.Errorf("查询 %s 服务状态失败: %w: %s", name, err, output)
	}
	return active, nil
}

func (systemServices) restart(ctx context.Context, name string) error {
	child, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if output, err := command.Output(child, nil, "systemctl", "restart", name); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, output)
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	_, err := commandOutput(ctx, "systemctl", "is-active", "--quiet", name)
	return err
}
