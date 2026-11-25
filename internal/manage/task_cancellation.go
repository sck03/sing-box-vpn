package manage

import (
	"context"
	"sync"
)

// Only probe contexts are registered. Side-effecting tasks finish under their
// existing budgets and cannot be interrupted by a panel cancellation request.
type runningProbes struct {
	mu     sync.Mutex
	cancel map[int64]context.CancelFunc
}

func (p *runningProbes) register(id int64, cancel context.CancelFunc) func() {
	p.mu.Lock()
	if p.cancel == nil {
		p.cancel = make(map[int64]context.CancelFunc)
	}
	p.cancel[id] = cancel
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		delete(p.cancel, id)
		p.mu.Unlock()
		cancel()
	}
}

func (p *runningProbes) stop(id int64) {
	p.mu.Lock()
	cancel := p.cancel[id]
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *PlatformService) CancelTask(ctx context.Context, id int64) error {
	if err := s.Store.Cancel(ctx, id); err != nil {
		return err
	}
	s.probes.stop(id)
	s.signal()
	return nil
}

func (s *PlatformService) runProbe(ctx context.Context, id int64, run func(context.Context) error) error {
	child, cancel := context.WithCancel(ctx)
	defer s.probes.register(id, cancel)()
	// Register before reading the durable request so cancellation cannot fall
	// between the worker claiming a task and exposing its context.
	requested, err := s.Store.CancellationRequested(child, id)
	if err != nil {
		return err
	}
	if requested {
		cancel()
	}
	if err := child.Err(); err != nil {
		return err
	}
	return run(child)
}
