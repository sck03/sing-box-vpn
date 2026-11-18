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

// Configuration and external side effects remain ordered. Read-only probes
// have their own bounded worker and never block a configuration repair.
func (s *PlatformService) runWorkers(ctx context.Context) error {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- s.runLane(child, "control") }()
	go func() { done <- s.runLane(child, "background") }()
	err := <-done
	cancel()
	return errors.Join(err, <-done)
}

func (s *PlatformService) runLane(ctx context.Context, lane string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	nextMaintenance, nextSubscriptions, nextRetention := time.Time{}, time.Time{}, time.Time{}
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
				if err != nil {
					credentialDelay = min(credentialDelay*2, 5*time.Minute)
				} else {
					credentialDelay = 5 * time.Second
				}
				nextCredentials = time.Now().Add(credentialDelay)
			}
			if time.Now().After(nextRetention) {
				nextRetention = time.Now().Add(time.Hour)
				maintenance, stop := context.WithTimeout(ctx, 30*time.Second)
				err := errors.Join(s.Accounts.PruneHistory(maintenance, time.Now()), s.Store.PruneHistory(maintenance, time.Now()), s.pruneCertificates(maintenance, time.Now()))
				if err != nil {
					_ = s.Auth.Audit(maintenance, "scheduler", "history-retention", "", "failed")
				}
				stop()
			}
			if time.Now().After(nextSubscriptions) {
				nextSubscriptions = time.Now().Add(time.Minute)
				if err := s.reconcileProgramUpgrade(ctx); err != nil {
					_ = s.Auth.Audit(ctx, "scheduler", "upgrade-receipt", "", "failed")
				}
				if err := s.Store.ExpireExternal(ctx, time.Now().Add(-upgradeWaitBudget)); err != nil {
					return err
				}
				if err := s.scheduleSubscriptions(ctx); err != nil {
					_ = s.Auth.Audit(ctx, "scheduler", "subscription-schedule", "", "failed")
				}
			}
			if !s.NoSystem && time.Now().After(nextMaintenance) {
				nextMaintenance = time.Now().Add(30 * time.Second)
				maintenance, stop := context.WithTimeout(ctx, 30*time.Second)
				if err := s.syncTunnel(maintenance); err != nil {
					_ = s.Auth.Audit(maintenance, "scheduler", "tunnel-sync", "", "failed")
				}
				if err := s.scheduleCertificates(maintenance); err != nil {
					_ = s.Auth.Audit(maintenance, "scheduler", "certificate-schedule", "", "failed")
				}
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
		if err = s.runTask(ctx, task); err != nil {
			return err
		}
	}
}

func (s *PlatformService) runTask(ctx context.Context, task controlstore.Task) error {
	s.invalidateOverview()
	budget := application.ControlTaskTimeout
	if task.Kind == "subscription" {
		var action PlatformAction
		if json.Unmarshal(task.Input, &action) == nil && action.Subscription.Operation == "check" {
			budget = application.ProbeTaskTimeout
		}
	}
	child, cancel := context.WithTimeout(ctx, budget)
	var err error
	if task.Kind == "apply" {
		var input applyInput
		err = json.Unmarshal(task.Input, &input)
		if err == nil {
			err = withServiceLock(child, s.Root, func() error { return s.apply(child, task.ID, input) })
		}
	} else {
		var action PlatformAction
		err = json.Unmarshal(task.Input, &action)
		if err == nil {
			if action.Operation == "outbound-probe" || action.Operation == "program-upgrade" || action.Operation == "subscription" {
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
	finish, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	result := "success"
	if err != nil {
		result = "failed"
	}
	_ = s.Auth.Audit(finish, task.Actor, task.Kind, fmt.Sprint(task.ID), result)
	finishErr := s.Store.Finish(finish, task.ID, err)
	s.invalidateOverview()
	return finishErr
}
