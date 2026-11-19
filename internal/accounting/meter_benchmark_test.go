package accounting

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

// Measures a warm, reusable reservation, excluding socket and disk refill work.
func BenchmarkMeterTakeComplete(b *testing.B) {
	ctx := context.Background()
	s, err := Open(filepath.Join(b.TempDir(), "control.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	if err = s.SaveAccount(ctx, domain.Account{ID: "a", Name: "A"}); err != nil {
		b.Fatal(err)
	}
	if err = s.Bind(ctx, domain.Credential{ID: "c", AccountID: "a", RuntimeID: "normal", InboundID: "in", Protocol: "vless", Generation: 1, UUID: "aabbccdd-1122-4333-8444-5566778899aa"}); err != nil {
		b.Fatal(err)
	}
	m := NewMeter(s, "normal")
	h, err := m.Open(ctx, "c.1", "in", &closedCounter{})
	if err != nil {
		b.Fatal(err)
	}
	n, err := h.Take(1024)
	if err != nil {
		b.Fatal(err)
	}
	h.Complete(n, 0, true)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		n, err := h.Take(1024)
		if err != nil {
			b.Fatal(err)
		}
		h.Complete(n, 0, true)
	}
	b.StopTimer()
	m.Shutdown()
	if err = m.Close(ctx); err != nil {
		b.Fatal(err)
	}
}
