// Package command captures bounded output from short-lived external commands.
package command

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

const maxOutput = 1 << 20
const waitDelay = time.Second

var ErrOutputLimit = errors.New("command output exceeds 1 MiB")

type capture struct {
	data     []byte
	overflow bool
}

func (c *capture) Write(p []byte) (int, error) {
	n := min(len(p), maxOutput-len(c.data))
	if size := len(c.data) + n; size > cap(c.data) {
		data := make([]byte, len(c.data), min(maxOutput, max(size, 2*cap(c.data))))
		copy(data, c.data)
		c.data = data
	}
	c.data = append(c.data, p[:n]...)
	c.overflow = c.overflow || n < len(p)
	// Keep draining after the limit so the child cannot block on a full pipe.
	return len(p), nil
}

// Output combines stdout/stderr and preserves exit and context errors. Callers
// set the execution deadline in ctx; inherited pipes get at most one extra second
// after cancellation or process exit. Truncated output is never a success.
// Input must be nil, a file, or a finite in-memory reader (not a blocking stream).
func Output(ctx context.Context, input io.Reader, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = input
	cmd.WaitDelay = waitDelay
	var output capture
	// os/exec serializes writes to the same comparable writer for both streams.
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if output.overflow {
		err = errors.Join(err, ErrOutputLimit)
	}
	return output.data, errors.Join(err, ctx.Err())
}
