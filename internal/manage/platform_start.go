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
	state, err := s.Store.State(ctx)
	if err != nil {
		return err
	}
	c, err := decodePlatform(state.Applied)
	if err != nil {
		return err
	}
	if err = withStartupLock(ctx, s.Root, func() error {
		if e := recoverSubscriptionRuntime(s.Root); e != nil {
			return e
		}
		files, e := s.buildApplied(ctx, c)
		if e != nil {
			return e
		}
		if e = files.json("runtime-revision.json", state.AppliedRevision); e != nil {
			return e
		}
		return publish(s.Root, files)
	}); err != nil {
		return err
	}
	s.workerReady.Store(true)
	defer s.workerReady.Store(false)
	return s.runWorkers(ctx)
}
