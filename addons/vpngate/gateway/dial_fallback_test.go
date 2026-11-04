package main

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestPublicTargetsFallBackAfterConnectionFailure(t *testing.T) {
	peer, success := net.Pipe()
	defer peer.Close()
	defer success.Close()
	var calls []string
	c, err := dialPublicTargets(context.Background(), "tcp", "443", []net.IP{
		net.ParseIP("10.0.0.1"), net.ParseIP("203.0.113.1"), net.ParseIP("::1"), net.ParseIP("203.0.113.2"),
	}, func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp4" {
			t.Fatal("unexpected address family", network)
		}
		calls = append(calls, address)
		if address == "203.0.113.1:443" {
			return nil, errors.New("first address is unreachable")
		}
		return success, nil
	})
	if err != nil || c != success || !reflect.DeepEqual(calls, []string{"203.0.113.1:443", "203.0.113.2:443"}) {
		t.Fatal("public address fallback failed or dialed a private address", calls, err)
	}
}

func TestPublicTargetsShareTimeoutBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	peer, success := net.Pipe()
	defer peer.Close()
	defer success.Close()
	calls := 0
	c, err := dialPublicTargets(ctx, "tcp", "443", []net.IP{net.ParseIP("203.0.113.1"), net.ParseIP("203.0.113.2")},
		func(attempt context.Context, _, _ string) (net.Conn, error) {
			calls++
			if calls == 1 {
				<-attempt.Done()
				return nil, attempt.Err()
			}
			if ctx.Err() != nil {
				t.Fatal("first attempt consumed the entire connection budget")
			}
			return success, nil
		})
	if err != nil || c != success || calls != 2 {
		t.Fatal("timed-out address prevented fallback", calls, err)
	}
}

func TestPublicTargetsStopOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	_, err := dialPublicTargets(ctx, "tcp", "443", []net.IP{net.ParseIP("203.0.113.1"), net.ParseIP("203.0.113.2")},
		func(context.Context, string, string) (net.Conn, error) {
			calls++
			cancel()
			return nil, context.Canceled
		})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("canceled dial retained further attempts", calls, err)
	}
}

func TestPublicTargetsRejectPrivateOnlyAnswers(t *testing.T) {
	_, err := dialPublicTargets(context.Background(), "udp", "53", []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("192.168.0.1")},
		func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("private-only DNS answer reached the dialer")
			return nil, nil
		})
	if err == nil {
		t.Fatal("private-only DNS answer was accepted")
	}
}
