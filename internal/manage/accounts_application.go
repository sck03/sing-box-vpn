package manage

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"path/filepath"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
)

type AccountService struct {
	Root    string
	Store   *accounting.Store
	Control configurationReader
	// Config supplies an explicit immutable candidate for compilation. Downloads
	// leave it nil and capture the confirmed configuration once per operation.
	Config *PlatformConfig
}

func (s *PlatformService) accountService() AccountService {
	return AccountService{Root: s.Root, Store: s.Accounts, Control: s.Store}
}

type bindingRepository struct{ *accounting.Store }

func (r bindingRepository) RequireAccount(ctx context.Context, id string) error {
	_, err := r.Account(ctx, id)
	return err
}

func (s AccountService) openStore() (*accounting.Store, func(), error) {
	if s.Store != nil {
		return s.Store, func() {}, nil
	}
	store, err := accounting.Open(filepath.Join(s.Root, "control.db"))
	if err != nil {
		return nil, func() {}, err
	}
	return store, func() { _ = store.Close() }, nil
}

func (s AccountService) Execute(ctx context.Context, c application.AccountCommand, out io.Writer) error {
	if c.UUID != "" {
		if _, err := uuid.Parse(c.UUID); err != nil {
			return errors.New("invalid custom UUID")
		}
	}
	if c.Password != "" && (len(c.Password) < 16 || len(c.Password) > 128) {
		return errors.New("custom password must contain 16–128 bytes")
	}
	store, closeStore, err := s.openStore()
	if err != nil {
		return err
	}
	defer closeStore()
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
	case "rotate":
		if c.UUID == "" {
			c.UUID = randomUUID()
		}
		if c.Password == "" {
			c.Password = randomSecret()
		}
		return store.Rotate(ctx, c.ID, c.UUID, c.Password, c.Generation)
	case "unbind":
		return store.Unbind(ctx, c.ID, c.Generation)
	default:
		return fmt.Errorf("unknown account command %q", c.Operation)
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
		return *s.Config, nil
	}
	if s.Control != nil || platformOwned(s.Root) {
		store := s.Control
		if store == nil {
			opened, err := controlstore.Open(filepath.Join(s.Root, "control.db"))
			if err != nil {
				return PlatformConfig{}, err
			}
			defer opened.Close()
			store = opened
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
	var app App
	if ok, err := ReadJSON(filepath.Join(s.Root, "app.json"), &app); err != nil {
		return PlatformConfig{}, err
	} else if !ok {
		return PlatformConfig{}, errors.New("ordinary node is not initialized")
	}
	settings, private, err := loadVPN(s.Root)
	return PlatformConfig{App: app, VPN: settings, VPNSecrets: private}, err
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
	return managedNodes(App{Profile: p, Inbounds: MakeInbounds(p, "vg-"+c.VPN.ActiveCountry+"-")})
}
