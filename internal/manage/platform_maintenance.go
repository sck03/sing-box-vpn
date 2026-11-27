package manage

import (
	"context"
	"time"
)

// Maintenance has one bounded worker. Host transitions still share the service
// lock, but receipt checks, scheduling and idle retries do not delay task claims.
func (s *PlatformService) runMaintenance(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	nextHost, nextSchedules, nextCredentials := time.Time{}, time.Time{}, time.Time{}
	credentialDelay := 5 * time.Second
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		if time.Now().After(nextSchedules) {
			schedule, stop := context.WithTimeout(ctx, 30*time.Second)
			s.schedulerResult(schedule, "panel-restart", s.reconcilePanelRestart(schedule))
			s.schedulerResult(schedule, "program-upgrade", s.reconcileProgramUpgrade(schedule))
			err := s.Store.ExpireExternal(schedule, time.Now().Add(-upgradeWaitBudget))
			if err == nil {
				s.schedulerResult(schedule, "subscription-schedule", s.scheduleSubscriptions(schedule))
			}
			stop()
			if err != nil {
				return err
			}
			nextSchedules = time.Now().Add(time.Minute)
		}
		if time.Now().After(nextCredentials) {
			reconcile, stop := context.WithTimeout(ctx, 60*time.Second)
			pending, err := s.Accounts.HasUnpublishedCredentials(reconcile)
			if err == nil && pending {
				err = withServiceLock(reconcile, s.Root, func() error { return s.reconcileCredentials(reconcile) })
			}
			stop()
			s.backgroundResult("credential-publication", err)
			if err != nil {
				credentialDelay = min(credentialDelay*2, 5*time.Minute)
			} else {
				credentialDelay = 5 * time.Second
			}
			nextCredentials = time.Now().Add(credentialDelay)
		}
		if !s.NoSystem && time.Now().After(nextHost) {
			maintenance, stop := context.WithTimeout(ctx, 30*time.Second)
			s.schedulerResult(maintenance, "tunnel-sync", s.syncTunnel(maintenance))
			s.schedulerResult(maintenance, "certificate-schedule", s.scheduleCertificates(maintenance))
			stop()
			nextHost = time.Now().Add(30 * time.Second)
		}
		next := nextCredentials
		if nextSchedules.Before(next) {
			next = nextSchedules
		}
		if !s.NoSystem && nextHost.Before(next) {
			next = nextHost
		}
		timer.Reset(max(0, time.Until(next)))
	}
}
