package application

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestDeploymentRecoversAfterCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var steps []string
	confirmed := false
	err := ApplyDeployment(ctx, Deployment{
		Phase:    func(_ context.Context, phase string) error { steps = append(steps, phase); return nil },
		Publish:  func(context.Context) error { return nil },
		Activate: func(context.Context) error { cancel(); return context.Canceled },
		Confirm:  func(context.Context) error { confirmed = true; return nil },
		Recover: func(ctx context.Context) error {
			if ctx.Err() != nil {
				t.Fatal("recovery inherited cancellation")
			}
			steps = append(steps, "recovered")
			return nil
		},
	})
	if !errors.Is(err, context.Canceled) || confirmed || !slices.Equal(steps, []string{"publishing", "applying-services", "recovered", "rolled-back"}) {
		t.Fatal(steps, err)
	}
}

func TestDeploymentDoesNotActivateFailedPublication(t *testing.T) {
	failure := errors.New("publication failed")
	err := ApplyDeployment(t.Context(), Deployment{
		Phase:    func(context.Context, string) error { return nil },
		Publish:  func(context.Context) error { return failure },
		Activate: func(context.Context) error { t.Fatal("activated failed publication"); return nil },
		Recover:  func(context.Context) error { t.Fatal("atomic publish failure changed host"); return nil },
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
}
