package manage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

func (s *PlatformService) Run(ctx context.Context) error {
	workerLock, err := os.OpenFile(filepath.Join(s.Root, ".platform-worker.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer workerLock.Close()
	if err = lockFile(workerLock); err != nil {
		return errors.New("another platform worker is already running")
	}
	if err := s.reconcilePanelRestart(ctx); err != nil {
		_ = s.Auth.Audit(ctx, "scheduler", "panel-restart-receipt", "", "failed")
	}
	if err := s.reconcileProgramUpgrade(ctx); err != nil {
		_ = s.Auth.Audit(ctx, "scheduler", "upgrade-receipt", "", "failed")
	}
	if err := s.Store.ExpireExternal(ctx, time.Now().Add(-upgradeWaitBudget)); err != nil {
		return err
	}
	if err := s.Store.Recover(ctx); err != nil {
		return err
	}
	// Restore projections from the confirmed database state before serving tasks.
	if err := s.recoverApplied(ctx); err != nil {
		return err
	}
	s.workerReady.Store(true)
	defer s.workerReady.Store(false)
	return s.runWorkers(ctx)
}

// Startup and explicit recovery read and publish one confirmed snapshot under
// the publication lock. Startup must not acquire the service transition lock:
// the restart helper holds it while waiting for this worker to become ready.
func (s *PlatformService) recoverApplied(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return withStartupLock(ctx, s.Root, func() error {
		if err := recoverSubscriptionRuntime(s.Root); err != nil {
			return err
		}
		c, state, err := s.appliedConfiguration(ctx)
		if err != nil {
			return err
		}
		files, err := s.buildApplied(ctx, c)
		if err != nil {
			return err
		}
		if err := files.json("runtime-revision.json", state.AppliedRevision); err != nil {
			return err
		}
		if err := publish(s.Root, files); err != nil {
			return err
		}
		s.invalidateOverview()
		return nil
	})
}
