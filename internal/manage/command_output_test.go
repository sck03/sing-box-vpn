package manage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/command"
)

func TestCommandOutputHelper(t *testing.T) {
	args := os.Args
	if len(args) < 2 || args[len(args)-2] != "--" {
		return
	}
	if args[len(args)-1] == "wait" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	size, err := strconv.Atoi(args[len(args)-1])
	if err != nil {
		os.Exit(2)
	}
	_, _ = io.CopyN(os.Stdout, bytes.NewReader(bytes.Repeat([]byte("x"), size)), int64(size))
	os.Exit(0)
}

func TestCommandOutputBoundaries(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 1 << 20, (1 << 20) + 1, 4 << 20} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			output, err := commandOutput(t.Context(), executable, "-test.run=^TestCommandOutputHelper$", "--", strconv.Itoa(size))
			if (err != nil) != (size > 1<<20) || len(output) != min(size, 1<<20) {
				t.Fatalf("size=%d: captured=%d, err=%v", size, len(output), err)
			}
			if size > 1<<20 && !errors.Is(err, command.ErrOutputLimit) {
				t.Fatal("output limit error was not preserved")
			}
		})
	}
}

func TestCommandOutputPreservesDeadline(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err = commandOutput(ctx, executable, "-test.run=^TestCommandOutputHelper$", "--", "wait")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("command lost the deadline error: %v", err)
	}
}
