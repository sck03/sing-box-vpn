package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOutputHelper(t *testing.T) {
	args := os.Args
	if len(args) < 2 || args[len(args)-2] != "--" {
		return
	}
	switch args[len(args)-1] {
	case "echo":
		_, _ = io.Copy(os.Stdout, os.Stdin)
		_, _ = io.WriteString(os.Stderr, "stderr")
	case "exit":
		_, _ = io.WriteString(os.Stderr, "failed")
		os.Exit(7)
	case "overflow":
		chunk := bytes.Repeat([]byte("x"), 32<<10)
		for i := 0; i < 128; i++ {
			_, _ = os.Stdout.Write(chunk)
			_, _ = os.Stderr.Write(chunk)
		}
		os.Exit(1)
	case "inherit":
		child := exec.Command(args[0], "-test.run=^TestOutputHelper$", "--", "hold")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			os.Exit(2)
		}
		fmt.Println(child.Process.Pid)
		_ = child.Process.Release()
	case "hold":
		// Also bounds the lifetime if a failing test cannot recover the PID.
		time.Sleep(5 * time.Second)
	}
	os.Exit(0)
}

func helperOutput(t *testing.T, ctx context.Context, input io.Reader, mode string) ([]byte, error) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Output(ctx, input, executable, "-test.run=^TestOutputHelper$", "--", mode)
}

func TestOutputStreamsAndExit(t *testing.T) {
	input := strings.Repeat("rule\n", 10000)
	output, err := helperOutput(t, t.Context(), strings.NewReader(input), "echo")
	if err != nil || string(output) != input+"stderr" {
		t.Fatalf("stdin or combined output lost: bytes=%d err=%v", len(output), err)
	}
	output, err = helperOutput(t, t.Context(), nil, "exit")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 || string(output) != "failed" {
		t.Fatalf("exit status or diagnostic lost: %q %v", output, err)
	}
}

func TestOutputOverflowPreservesExit(t *testing.T) {
	output, err := helperOutput(t, t.Context(), nil, "overflow")
	var exit *exec.ExitError
	if !errors.Is(err, ErrOutputLimit) || !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("overflow/exit error lost: %v", err)
	}
	if len(output) != maxOutput || cap(output) > maxOutput || bytes.Count(output, []byte("x")) != maxOutput {
		t.Fatalf("unexpected retained output: len=%d cap=%d", len(output), cap(output))
	}
}

func TestOutputInheritedPipe(t *testing.T) {
	// Avoid the race runtime's deliberate exit sleep in this timing boundary test.
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	output, err := helperOutput(t, ctx, nil, "inherit")
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(output)))
	if parseErr != nil {
		t.Fatalf("missing inherited-pipe holder PID: %q %v", output, err)
	}
	if process, findErr := os.FindProcess(pid); findErr == nil {
		_ = process.Kill()
		_, _ = process.Wait()
	}
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("inherited pipe did not stop at WaitDelay: %v", err)
	}
}

func TestOutputStartErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := helperOutput(t, ctx, nil, "echo"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled command started or lost cancellation: %v", err)
	}
	if _, err := Output(t.Context(), nil, filepath.Join(t.TempDir(), "missing")); !errors.Is(err, os.ErrNotExist) && !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing executable error lost: %v", err)
	}
}

func BenchmarkOutputCapture(b *testing.B) {
	chunk := bytes.Repeat([]byte("x"), 32<<10)
	for _, size := range []int{4 << 10, 8 << 20} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			for _, bounded := range []bool{false, true} {
				b.Run(fmt.Sprintf("bounded=%t", bounded), func(b *testing.B) {
					b.SetBytes(int64(size))
					b.ReportAllocs()
					for b.Loop() {
						var output io.Writer
						if bounded {
							output = new(capture)
						} else {
							output = new(bytes.Buffer)
						}
						for offset := 0; offset < size; offset += len(chunk) {
							_, _ = output.Write(chunk[:min(len(chunk), size-offset)])
						}
					}
				})
			}
		})
	}
}
