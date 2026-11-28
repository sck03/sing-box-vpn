package application

import (
	"context"
	"slices"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

type CredentialPublicationRepository interface {
	CredentialPublications(context.Context) ([]domain.CredentialPublication, error)
	CredentialsPublished(context.Context, string, int64) error
}

// The application owns ordering and retry policy; adapters own file validation,
// atomic publication, host observations and runtime acknowledgements.
type CredentialPublicationTarget struct {
	Publish     func(context.Context, []domain.CredentialPublication) error
	Active      func(context.Context) ([]string, error)
	AwaitLoaded func(context.Context, string, int64) error
}

// ReconcileCredentials consumes durable intent without replaying account
// mutations. An intentionally stopped runtime is never started by reconciliation.
func ReconcileCredentials(ctx context.Context, repository CredentialPublicationRepository, target CredentialPublicationTarget) error {
	versions, err := repository.CredentialPublications(ctx)
	if err != nil {
		return err
	}
	pending := make([]domain.CredentialPublication, 0, len(versions))
	for _, version := range versions {
		if version.Published < version.Revision {
			pending = append(pending, version)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if err = target.Publish(ctx, pending); err != nil {
		return err
	}
	for _, version := range pending {
		if err = repository.CredentialsPublished(ctx, version.Runtime, version.Revision); err != nil {
			return err
		}
	}
	active, err := target.Active(ctx)
	if err != nil {
		return err
	}
	for _, version := range versions {
		if version.Loaded < version.Revision && slices.Contains(active, version.Runtime) {
			if err = target.AwaitLoaded(ctx, version.Runtime, version.Revision); err != nil {
				return err
			}
		}
	}
	return nil
}
