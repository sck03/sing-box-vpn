package main

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagerStopWaitsForBackgroundRefresh(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	m := newManager(cfg, "")
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	m.sourceClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
		<-release // Simulate transport cleanup that outlives cancellation.
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.run(ctx) }()
	defer func() { close(release); cancel(); <-done }()
	m.scheduleRefresh(ctx, cfg, true)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not start")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("background refresh was not cancelled")
	}
	select {
	case <-done:
		t.Fatal("manager returned while background refresh still held resources")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStoppedManagerRejectsBackgroundRefresh(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	m := newManager(cfg, "")
	var calls atomic.Int32
	m.sourceClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return testHTTPResponse("invalid source"), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.run(ctx)
	m.scheduleRefresh(context.Background(), cfg, true)
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("stopped manager accepted new refresh work")
	}
}
