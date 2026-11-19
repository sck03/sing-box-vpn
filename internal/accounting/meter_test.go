package accounting

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

type closedCounter struct{ count atomic.Int32 }

func (c *closedCounter) Close() error { c.count.Add(1); return nil }

func meterFixture(t *testing.T, quota int64) (*Store, *Meter, *Handle, *closedCounter) {
	t.Helper()
	s, _ := fixture(t, quota)
	if err := s.Bind(t.Context(), domain.Credential{ID: "credential", AccountID: "alice", RuntimeID: "normal", InboundID: "in", Protocol: "vless", Generation: 1, UUID: "aabbccdd-1122-4333-8444-5566778899aa"}); err != nil {
		t.Fatal(err)
	}
	m := NewMeter(s, "normal")
	closer := &closedCounter{}
	h, err := m.Open(t.Context(), "credential.1", "in", closer)
	if err != nil {
		t.Fatal(err)
	}
	return s, m, h, closer
}

func TestMeterIdleSessionAndReservationBound(t *testing.T) {
	s, m, h, _ := meterFixture(t, 1<<20)
	u, _ := s.Account(t.Context(), "alice")
	if u.Charged != 0 {
		t.Fatal("idle session consumed quota")
	}
	n, err := h.Take(int(ReservationBytes))
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		n, err := h.Take(1)
		if err == nil {
			h.Complete(n, n, false)
		}
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatal("in-flight reservation exceeded bound", err)
	case <-time.After(30 * time.Millisecond):
	}
	h.Complete(n, 0, true)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	u, _ = s.Account(t.Context(), "alice")
	if u.Charged != ReservationBytes {
		t.Fatal(u.Charged)
	}
	m.Shutdown()
	if err = m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, _ = s.Account(t.Context(), "alice")
	if u.Charged != 1 || u.Download != 1 {
		t.Fatal(u)
	}
}

func TestPacketReservationRefillsWithoutTruncating(t *testing.T) {
	_, m, h, _ := meterFixture(t, 200000)
	n, err := h.Take(int(ReservationBytes) - 20)
	if err != nil {
		t.Fatal(err)
	}
	h.Complete(n, n, true)
	n, err = h.TakePacket(100)
	if err != nil || n != 100 {
		t.Fatal(n, err)
	}
	h.Complete(n, n, false)
	m.Shutdown()
	if err = m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaAndDisabledEmptyPackets(t *testing.T) {
	s, m, h, c := meterFixture(t, 10)
	n, err := h.Take(20)
	if err != nil || n != 10 {
		t.Fatal(n, err)
	}
	h.Complete(n, n, true)
	if _, err = h.Take(1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	u, _ := s.Account(t.Context(), "alice")
	u.Disabled = true
	if err = s.SaveAccount(t.Context(), u.Account); err != nil {
		t.Fatal(err)
	}
	if _, err = h.TakePacket(0); !errors.Is(err, ErrDenied) {
		t.Fatal("disabled empty datagram admitted", err)
	}
	if err = m.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c.count.Load() == 0 {
		t.Fatal("disabled session not closed")
	}
	m.Shutdown()
	if err = m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestMeterRejectsCrossRuntimeOrInboundIdentity(t *testing.T) {
	s, m, _, c := meterFixture(t, 1000)
	for _, input := range []struct{ user, inbound string }{{"credential.2", "in"}, {"credential.1", "wrong"}, {"unknown.1", "in"}} {
		if _, err := m.Open(t.Context(), input.user, input.inbound, c); !errors.Is(err, ErrDenied) {
			t.Fatal(input, err)
		}
	}
	other := NewMeter(s, "vpngate")
	if _, err := other.Open(t.Context(), "credential.1", "in", c); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}
