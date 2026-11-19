package application

import (
	"context"
	"errors"
	"time"
)

// Deployment separates orchestration from persistence, files and host services.
// Publish must be atomic. Recovery restores only configuration and service state;
// credential revocations and accounting are never rolled back.
type Deployment struct {
	Phase    func(context.Context, string) error
	Publish  func(context.Context) error
	Activate func(context.Context) error
	Confirm  func(context.Context) error
	Recover  func(context.Context) error
}

func ApplyDeployment(ctx context.Context, d Deployment) error {
	if err := d.Phase(ctx, "publishing"); err != nil {
		return err
	}
	if err := d.Publish(ctx); err != nil {
		return err
	}
	err := d.Phase(ctx, "applying-services")
	if err == nil {
		err = d.Activate(ctx)
	}
	if err == nil {
		err = d.Confirm(ctx)
	}
	if err == nil {
		return nil
	}
	recovery, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	rollback := d.Recover(recovery)
	phase := "rolled-back"
	if rollback != nil {
		phase = "rollback-failed"
	}
	return errors.Join(err, rollback, d.Phase(recovery, phase))
}
