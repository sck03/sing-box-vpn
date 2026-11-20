package manage

import (
	"context"
	"fmt"
	"time"

	"github.com/sck03/sing-box-vpn/internal/command"
)

type systemServices struct{}

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
