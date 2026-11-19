package manage

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
)

func queryUnit(ctx context.Context, unit, query string, negative ...int) (bool, error) {
	data, err := commandOutput(ctx, "systemctl", query, "--quiet", unit)
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if len(data) == 0 && errors.As(err, &exit) && slices.Contains(negative, exit.ExitCode()) {
		return false, nil
	}
	return false, fmt.Errorf("query service %s: %w: %s", unit, err, data)
}
