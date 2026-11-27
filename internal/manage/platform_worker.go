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
	done := make(chan error, 4)
	go func() { done <- s.runLane(child, "control") }()
	go func() { done <- s.runLane(child, application.SubscriptionLane) }()
	go func() { done <- s.runLane(child, application.ProbeLane) }()
	go func() { done <- s.runRetention(child) }()
	err := <-done
	cancel()
	return errors.Join(err, <-done, <-done, <-done)
}

func (s *PlatformService) runLane(ctx context.Context, lane string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	nextMaintenance, nextSubscriptions := time.Time{}, time.Time{}
	nextCredentials, credentialDelay := time.Time{}, 5*time.Second
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Schedule between tasks as well as while idle; a busy queue must not
		// starve refresh or renewal. Store deduplication bounds automatic work.
		if lane == "control" {
			if time.Now().After(nextCredentials) {
				reconcile, stop := context.WithTimeout(ctx, 60*time.Second)
				err := withServiceLock(reconcile, s.Root, func() error { return s.reconcileCredentials(reconcile) })
				stop()
				s.backgroundResult("credential-publication", err)
				if err != nil {
					credentialDelay = min(credentialDelay*2, 5*time.Minute)
				} else {
					credentialDelay = 5 * time.Second
				}
				nextCredentials = time.Now().Add(credentialDelay)
			}
			if time.Now().After(nextSubscriptions) {
				s.schedulerResult(ctx, "panel-restart", s.reconcilePanelRestart(ctx))
				nextSubscriptions = time.Now().Add(time.Minute)
				s.schedulerResult(ctx, "program-upgrade", s.reconcileProgramUpgrade(ctx))
				if err := s.Store.ExpireExternal(ctx, time.Now().Add(-upgradeWaitBudget)); err != nil {
					return err
				}
				s.schedulerResult(ctx, "subscription-schedule", s.scheduleSubscriptions(ctx))
			}
			if !s.NoSystem && time.Now().After(nextMaintenance) {
				nextMaintenance = time.Now().Add(30 * time.Second)
				maintenance, stop := context.WithTimeout(ctx, 30*time.Second)
				s.schedulerResult(maintenance, "tunnel-sync", s.syncTunnel(maintenance))
				s.schedulerResult(maintenance, "certificate-schedule", s.scheduleCertificates(maintenance))
				stop()
			}
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
