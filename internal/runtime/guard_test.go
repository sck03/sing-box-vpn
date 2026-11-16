package runtime

import (
	"bytes"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

type memoryConn struct {
	net.Conn
	input  *bytes.Reader
	output bytes.Buffer
	closed bool
}

func (c *memoryConn) Read(p []byte) (int, error)  { return c.input.Read(p) }
func (c *memoryConn) Write(p []byte) (int, error) { return c.output.Write(p) }
func (c *memoryConn) Close() error                { c.closed = true; return nil }

func TestStreamReservationBoundaries(t *testing.T) {
	for _, quota := range []int64{300000, 70001} {
		t.Run(strconv.FormatInt(quota, 10), func(t *testing.T) {
			store, err := accounting.Open(filepath.Join(t.TempDir(), "control.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ctx := t.Context()
			if err = store.SaveAccount(ctx, domain.Account{ID: "a", Name: "A", QuotaBytes: &quota}); err != nil {
				t.Fatal(err)
			}
			if err = store.Bind(ctx, domain.Credential{ID: "c", AccountID: "a", RuntimeID: "normal", InboundID: "in", Protocol: "vless", Generation: 1, UUID: "test"}); err != nil {
				t.Fatal(err)
			}
			payload := bytes.Repeat([]byte("abcdefg"), 20000)
			conn := &memoryConn{input: bytes.NewReader(payload)}
			meter := accounting.NewMeter(store, "normal")
			h, err := meter.Open(ctx, "c.1", "in", conn)
			if err != nil {
				t.Fatal(err)
			}
			guard := &stream{Conn: conn, handle: h}
			// A non-aligned first read leaves a short reservation for the next read.
			first := make([]byte, 123)
			if n, err := guard.Read(first); err != nil || n != len(first) {
				t.Fatal(n, err)
			}
			rest, readErr := io.ReadAll(guard)
			want := min(int64(len(payload)), quota)
			if got := append(first, rest...); !bytes.Equal(got, payload[:want]) {
				t.Fatalf("read length %d, want %d", len(got), want)
			}
			if quota < int64(len(payload)) && !errors.Is(readErr, accounting.ErrDenied) {
				t.Fatal(readErr)
			}
			written := 0
			if readErr == nil {
				written, err = guard.Write(payload)
				if err != nil || written != len(payload) || !bytes.Equal(conn.output.Bytes(), payload) {
					t.Fatal(written, err)
				}
			}
			guard.Close()
			if err = meter.Close(ctx); err != nil {
				t.Fatal(err)
			}
			u, err := store.Account(ctx, "a")
			if err != nil || u.Upload != want || u.Download != int64(written) || u.Charged != want+int64(written) {
				t.Fatal(u, err)
			}
		})
	}
}
