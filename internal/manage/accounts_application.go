package manage

import (
	"context"
	"errors"
	"io"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
)

type AccountService struct {
	Root    string
	Core    string
	Store   *accounting.Store
	Control configurationReader
	// Config supplies an explicit immutable candidate for compilation. Downloads
	// leave it nil and capture the confirmed configuration once per operation.
	Config   *PlatformConfig
	view     *subscriptionView
	exports  *accountExportCache
	bindings *accountBindingCache
}

func (s *PlatformService) accountService() AccountService {
	return AccountService{Root: s.Root, Core: s.Core, Store: s.Accounts, Control: s.Store, view: &s.exportView, exports: &s.exportPlans, bindings: &s.bindingView}
}

type bindingRepository struct{ *accounting.Store }

func (r bindingRepository) RequireAccount(ctx context.Context, id string) error {
	_, err := r.Account(ctx, id)
	return err
}

func (s AccountService) requireStore() (*accounting.Store, error) {
	if s.Store == nil {
		return nil, errors.New("account repository dependency is required")
	}
	return s.Store, nil
}

func (s AccountService) Execute(ctx context.Context, c application.AccountCommand, out io.Writer) error {
	store, err := s.requireStore()
	if err != nil {
		return err
	}
	s.Store = store
	switch c.Operation {
	case "bind":
		return withStartupLock(ctx, s.Root, func() error {
			entries, err := s.accountEntries(ctx, c.Scope)
			if err != nil {
				return err
			}
			available := make([]application.BindingEntry, 0, len(entries))
			for _, entry := range entries {
				available = append(available, application.BindingEntry{ID: entry.ID, Protocol: entry.Protocol, Disabled: entry.Disabled})
			}
			result, err := application.BindAccounts(ctx, bindingRepository{store}, c, available)
			if err != nil {
				return err
			}
			return jsonEncoder(out).Encode(result)
		})
	default:
		return application.MutateCredential(ctx, store, c)
	}

}

func (s AccountService) accountEntries(ctx context.Context, scope string) ([]ManagedNode, error) {
	c, err := s.configSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return configuredEntries(c, scope)
}

func (s AccountService) configSnapshot(ctx context.Context) (PlatformConfig, error) {
	if s.Config != nil {
		c := *s.Config
		c.normalize()
		return c, nil
	}
	store := s.Control
	if store == nil {
		return PlatformConfig{}, errors.New("configuration repository dependency is required")
	}
	data, err := store.AppliedConfig(ctx)
	if err != nil {
		return PlatformConfig{}, err
	}
	c, err := decodePlatform(data)
	if err != nil {
		return c, err
	}
	return observedConfig(ctx, store, c)
}

func configuredEntries(c PlatformConfig, scope string) ([]ManagedNode, error) {
	if scope == normalScope {
		return managedNodes(c.App)
	}
	if scope != vpngateScope {
		return nil, errors.New("invalid runtime scope")
	}
	p, ok := c.VPN.Profiles[c.VPN.ActiveCountry]
	if !ok {
		return nil, errors.New("VPNGate has no selected country")
	}
	p.Secrets = c.VPNSecrets[c.VPN.ActiveCountry]
	return MakeNodes(p, "vg-"+c.VPN.ActiveCountry+"-"), nil
}
