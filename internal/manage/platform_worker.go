package manage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
)

// Configuration, subscription publication and probes each have a bounded lane.
// Slow probes cannot occupy the subscription worker or block configuration repair.
func (s *PlatformService) runWorkers(ctx context.Context) error {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	workers := []func(context.Context) error{
		func(ctx context.Context) error { return s.runLane(ctx, application.ControlLane) },
		func(ctx context.Context) error { return s.runLane(ctx, application.SubscriptionLane) },
		func(ctx context.Context) error { return s.runLane(ctx, application.ProbeLane) },
		s.runRetention,
		s.runMaintenance,
	}
	done := make(chan error, len(workers))
	for _, run := range workers {
		go func() { done <- run(child) }()
	}
	var result error
	for range workers {
		result = errors.Join(result, <-done)
		cancel()
	}
	return result
}

func (s *PlatformService) runLane(ctx context.Context, lane string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		task, err := s.Store.ClaimLane(ctx, lane)
		if errors.Is(err, sql.ErrNoRows) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-s.wake:
			case <-ticker.C:
			}
			continue
		}
		if err != nil {
			return err
		}
		if task.Kind == "account" {
			followers, claimErr := s.Store.ClaimAccountFollowers(ctx, task.ID)
			if claimErr != nil {
				return claimErr
			}
			err = s.runAccountBatch(ctx, append([]controlstore.ClaimedTask{task}, followers...))
		} else {
			err = s.runTask(ctx, task)
		}
		if err != nil {
			return err
		}
	}
}

func (s *PlatformService) runTask(ctx context.Context, task controlstore.ClaimedTask) error {
	s.invalidateOverview()
	budget := application.ControlTaskTimeout
	if task.Kind == "subscription" || task.Kind == "outbound-probe" {
		var action taskInput
		if json.Unmarshal(task.Input, &action) == nil && action.Lane() == application.ProbeLane {
			budget = application.ProbeTaskTimeout
		}
	}
	child, cancel := context.WithTimeout(ctx, budget)
	var err error
	if task.Kind == "panel-restart" {
		err = s.startPanelRestart(child, task.ID, task.Input)
	} else if task.Kind == "apply" {
		var input applyInput
		err = json.Unmarshal(task.Input, &input)
		if err == nil {
			err = withServiceLock(child, s.Root, func() error { return s.apply(child, task.ID, input) })
		}
	} else {
		var action taskInput
		err = json.Unmarshal(task.Input, &action)
		if err == nil {
			if action.Lane() == application.ProbeLane {
				err = s.runProbe(child, task.ID, func(probe context.Context) error { return s.execute(probe, task.ID, action) })
			} else if action.Operation == "program-upgrade" || action.Operation == "subscription" {
				err = s.execute(child, task.ID, action)
			} else {
				err = withServiceLock(child, s.Root, func() error { return s.execute(child, task.ID, action) })
			}
		}
	}
	cancel()
	if errors.Is(err, errTaskDeferred) {
		return nil
	}
	return s.finishTask(task.Task, err)
}

func (s *PlatformService) finishTask(task controlstore.Task, err error) error {
	finish, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	finishErr := s.Store.Finish(finish, task.ID, err)
	result := "success"
	if err != nil || finishErr != nil {
		result = "failed"
	}
	if finishErr == nil {
		if final, readErr := s.Store.Task(finish, task.ID); readErr == nil && final.State == "cancelled" {
			result = "cancelled"
		}
	}
	s.backgroundResult("audit-write", s.Auth.Audit(finish, task.Actor, task.Kind, fmt.Sprint(task.ID), result))
	s.invalidateOverview()
	return finishErr
}

// Each command commits once. Publication is shared, while individual failures
// remain visible; interrupted or failed publication never replays mutations.
func (s *PlatformService) runAccountBatch(ctx context.Context, tasks []controlstore.ClaimedTask) error {
	child, cancel := context.WithTimeout(ctx, application.ControlTaskTimeout)
	defer cancel()
	failures := make([]error, len(tasks))
	lockErr := withServiceLock(child, s.Root, func() error {
		changed := false
		for i, task := range tasks {
			var action taskInput
			failures[i] = json.Unmarshal(task.Input, &action)
			if failures[i] == nil && (action.Operation != "account" || action.Automatic) {
				failures[i] = errors.New("invalid account task")
			}
			if failures[i] == nil {
				failures[i] = action.Validate()
			}
			if failures[i] == nil {
				failures[i] = s.mutateAccount(child, task.ID, action.Account)
			}
			changed = changed || failures[i] == nil
		}
		if changed {
			return s.reconcileCredentials(child)
		}
		return nil
	})
	var result error
	for i, task := range tasks {
		if failures[i] == nil && lockErr != nil {
			failures[i] = fmt.Errorf("credential publication incomplete; inspect task and repository before retrying: %w", lockErr)
		}
		result = errors.Join(result, s.finishTask(task.Task, failures[i]))
	}
	return result
}
