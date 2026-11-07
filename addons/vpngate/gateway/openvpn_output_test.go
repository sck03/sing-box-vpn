package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestOpenVPNOutputFragmentedReadiness(t *testing.T) {
	data := []byte("startup\r\n" + openVPNReady + "\r\n" + openVPNReady + "\nlast line")
	for size := 1; size <= len(data); size++ {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			var log bytes.Buffer
			ready := make(chan struct{})
			output := &openVPNOutput{log: &log, ready: ready}
			for offset := 0; offset < len(data); offset += size {
				chunk := data[offset:min(offset+size, len(data))]
				if n, err := output.Write(chunk); err != nil || n != len(chunk) {
					t.Fatalf("write: %d, %v", n, err)
				}
			}
			select {
			case <-ready:
			default:
				t.Fatal("fragmented readiness marker was lost")
			}
			if !bytes.Equal(log.Bytes(), data) {
				t.Fatal("log lost its final line or changed line endings")
			}
		})
	}
}

func TestOpenVPNOutputBoundedLongLine(t *testing.T) {
	var log bytes.Buffer
	ready := make(chan struct{})
	output := &openVPNOutput{log: &log, ready: ready}
	chunk := bytes.Repeat([]byte("x"), 32<<10)
	_, _ = output.Write([]byte("prefix\n"))
	for i := 0; i < 100; i++ {
		if n, err := output.Write(chunk); err != nil || n != len(chunk) {
			t.Fatal("long line stopped draining", n, err)
		}
	}
	if log.Len() != maxOpenVPNLog {
		t.Fatal("log exceeded its byte budget", log.Len())
	}
	select {
	case <-ready:
		t.Fatal("ordinary output signaled readiness")
	default:
	}
	_, _ = output.Write([]byte(openVPNReady[:len(openVPNReady)-1]))
	_, _ = output.Write(nil)
	select {
	case <-ready:
		t.Fatal("incomplete marker signaled readiness")
	default:
	}
	_, _ = output.Write([]byte(openVPNReady[len(openVPNReady)-1:]))
	select {
	case <-ready:
	default:
		t.Fatal("full log stopped readiness detection")
	}
}

type failingVPNLog struct {
	calls int
	err   error
}

func (w *failingVPNLog) Write([]byte) (int, error) { w.calls++; return 0, w.err }

func TestOpenVPNOutputIgnoresLogFailure(t *testing.T) {
	for _, err := range []error{errors.New("disk full"), nil} {
		log := &failingVPNLog{err: err}
		ready := make(chan struct{})
		output := &openVPNOutput{log: log, ready: ready}
		_, _ = output.Write([]byte("startup\n"))
		if n, err := output.Write([]byte(openVPNReady)); n != len(openVPNReady) || err != nil {
			t.Fatal("log failure propagated to process pipe", n, err)
		}
		if log.calls != 1 {
			t.Fatal("failed log was retried", log.calls)
		}
		select {
		case <-ready:
		default:
			t.Fatal("log failure stopped readiness detection")
		}
	}
}

func TestOpenVPNOutputProcess(t *testing.T) {
	if os.Getenv("YG_TEST_OPENVPN_OUTPUT") == "1" {
		chunk := bytes.Repeat([]byte("x"), 32<<10)
		for i := 0; i < 100; i++ {
			if _, err := os.Stdout.Write(chunk); err != nil {
				os.Exit(2)
			}
		}
		_, _ = io.WriteString(os.Stdout, "\n"+openVPNReady[:10])
		_, _ = io.WriteString(os.Stderr, openVPNReady[10:]+"\nlast line without newline")
		os.Exit(0)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestOpenVPNOutputProcess$")
	cmd.Env = append(os.Environ(), "YG_TEST_OPENVPN_OUTPUT=1")
	cmd.WaitDelay = time.Second
	logPath := filepath.Join(t.TempDir(), "openvpn.log")
	file, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ready := make(chan struct{})
	cmd.Stdout = &openVPNOutput{log: file, ready: ready}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done, exited := make(chan error, 1), make(chan error, 1)
	go waitProcess(cmd, done, exited, func() { file.Close() })
	if err := <-done; err != nil {
		t.Fatal("process did not drain oversized output", err)
	}
	select {
	case <-ready:
	default:
		t.Fatal("readiness spanning stdout/stderr was lost")
	}
	if _, err := file.Write([]byte("closed")); !errors.Is(err, os.ErrClosed) {
		t.Fatal("exit notification preceded log close", err)
	}
	info, err := os.Stat(logPath)
	if err != nil || info.Size() != maxOpenVPNLog {
		t.Fatal("unexpected final log size", info, err)
	}
}

func BenchmarkOpenVPNOutput(b *testing.B) {
	data := bytes.Repeat([]byte("2026-11-08 OpenVPN diagnostic message\n"), 4096)
	data = append(data, openVPNReady...)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output := openVPNOutput{log: io.Discard, ready: make(chan struct{})}
		for offset := 0; offset < len(data); offset += 32 << 10 {
			_, _ = output.Write(data[offset:min(offset+32<<10, len(data))])
		}
	}
}
