package manage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
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
		files, err := s.buildCredentialPublication(ctx, config, versions)
		if err != nil {
			return err
		}
		if err = s.checkRuntime(ctx, files); err != nil {
			return err
		}
		if err = withStartupLock(ctx, s.Root, func() error { return publishPlatformFiles(s.Root, files) }); err != nil {
			return err
		}
		for _, v := range versions {
			if v.Published >= v.Revision {
				continue
			}
			if err = s.Accounts.CredentialsPublished(ctx, v.Runtime, v.Revision); err != nil {
				return err
			}
		}
	}
	for _, v := range versions {
		if v.Loaded < v.Revision && slices.Contains(active, scopeService(v.Runtime)) {
			if err = s.awaitCredentialLoad(ctx, v.Runtime, v.Revision); err != nil {
				return err
			}
		}
	}
	s.invalidateOverview()
	return nil
}

// Credential changes leave routing, certificates and unrelated runtimes intact.
// Rebuild subscriptions only when an account source consumes a changed runtime.
func (s *PlatformService) buildCredentialPublication(ctx context.Context, c PlatformConfig, versions []accounting.CredentialPublication) (fileBundle, error) {
	c.normalize()
	if err := c.validate(false); err != nil {
		return nil, err
	}
	var err error
	c, err = observedConfig(ctx, s.Store, c)
	if err != nil {
		return nil, err
	}
	changed := map[string]bool{}
	files := fileBundle{}
	for _, v := range versions {
		if v.Published >= v.Revision {
			continue
		}
		changed[v.Runtime] = true
		var server Object
		var name string
		switch v.Runtime {
		case normalScope:
			name = "sb.json"
			server, err = serverForApp(c.App)
		case vpngateScope:
			if c.VPN.ActiveCountry == "" {
				continue
			}
			name = "vpngate/server.json"
			server, err = vpnServer(c.VPN, c.VPNSecrets)
		default:
			return nil, errors.New("unknown credential runtime")
		}
		if err != nil {
			return nil, err
		}
		if err = files.json(name, server); err != nil {
			return nil, err
		}
	}
	needsSubscriptions := slices.ContainsFunc(c.Subscriptions.Sources, func(source sub.Source) bool {
		return source.Kind == "account" && (source.Scope == "" || changed[source.Scope])
	})
	if needsSubscriptions {
		runtime, err := loadSubscriptionRuntime(s.Root)
		if err != nil {
			return nil, err
		}
		subscriptions, _, _, err := compileSubscriptions(ctx, s.Root, s.Core, c.Subscriptions, runtime, nil, sub.Available, AccountService{Root: s.Root, Core: s.Core, Store: s.Accounts, Control: s.Store, Config: &c})
		if err != nil {
			return nil, err
		}
		maps.Copy(files, subscriptions)
	}
	return files, nil
}

func (s *PlatformService) awaitCredentialLoad(ctx context.Context, scope string, revision int64) error {
	child, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		v, err := s.Accounts.CredentialPublication(child, scope)
		if err != nil {
			return err
		}
		if v.Loaded >= revision {
			return nil
		}
		select {
		case <-child.Done():
			return fmt.Errorf("%s credential revision %d is published but not loaded: %w", scope, revision, child.Err())
		case <-ticker.C:
		}
	}
}
