package manage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
)

func (s *PlatformService) executeAccount(ctx context.Context, id int64, command application.AccountCommand) error {
	if err := s.mutateAccount(ctx, id, command); err != nil {
		return err
	}
	if err := s.reconcileCredentials(ctx); err != nil {
		return fmt.Errorf("credentials saved; publication will retry automatically: %w", err)
	}
	return nil
}

func (s *PlatformService) mutateAccount(ctx context.Context, id int64, command application.AccountCommand) error {
	if command.Operation != "bind" && command.Scope != "" {
		found, err := s.Accounts.HasCredential(ctx, command.Scope, command.ID)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("credential belongs to a different runtime or is missing")
		}
	}
	var out bytes.Buffer
	if err := s.accountService().Execute(ctx, command, &out); err != nil {
		return err
	}
	if out.Len() > 0 {
		if err := s.Store.Result(ctx, id, out.Bytes()); err != nil {
			return err
		}
	}
	if err := s.Store.Phase(ctx, id, "publishing-credentials"); err != nil {
		return err
	}

	return nil
}

// Reconcile durable intent, never replay bind/rotate/unbind. This also runs on
// startup and after interrupted tasks. Deliberately stopped cores stay stopped.
func (s *PlatformService) reconcileCredentials(ctx context.Context) error {
	versions, err := s.Accounts.CredentialPublications(ctx)
	if err != nil {
		return err
	}
	var active []string
	pending := false
	for _, v := range versions {
		pending = pending || v.Published < v.Revision || (!s.NoSystem && v.Loaded < v.Revision)
	}
	if !pending {
		return nil
	}
	if !s.NoSystem {
		active, err = s.activeServices(ctx)
		if err != nil {
			return err
		}
	}
	needsPublish := slices.ContainsFunc(versions, func(v accounting.CredentialPublication) bool { return v.Published < v.Revision })
	if needsPublish {
		state, err := s.Store.State(ctx)
		if err != nil {
			return err
		}
		config, err := decodePlatform(state.Applied)
		if err != nil {
			return err
		}
		files, err := s.build(ctx, config)
		if err != nil {
			return err
		}
		if err = s.checkRuntime(ctx, files); err != nil {
			return err
		}
		if err = withStartupLock(ctx, s.Root, func() error { return publish(s.Root, files) }); err != nil {
			return err
		}
		for _, v := range versions {
			if err = s.Accounts.CredentialsPublished(ctx, v.Runtime, v.Revision); err != nil {
				return err
			}
		}
	}
	for _, v := range versions {
		if v.Loaded < v.Revision && slices.Contains(active, scopeService(v.Runtime)) {
			if err = s.service(ctx, scopeService(v.Runtime), "restart"); err != nil {
				return err
			}
		}
	}
	s.invalidateOverview()
	return nil
}
