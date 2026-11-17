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
)

type AccountService struct {
	Root  string
	Store *accounting.Store
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
	case "list":
		accounts, err := s.List(ctx)
		if err != nil {
			return err
		}
		return jsonEncoder(out).Encode(accounts)
	case "save":
		return store.SaveAccount(ctx, c.Account)
	case "bind":
		return withStartupLock(ctx, s.Root, func() error {
			entries, err := s.accountEntries(c.Scope)
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
	case "credentials":
		result, err := s.Credentials(ctx, c.ID, c.Scope)
		if err != nil {
			return err
		}
		return jsonEncoder(out).Encode(result)
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
	case "export":
		artifact, err := s.Export(ctx, c)
		if err != nil {
			return err
		}
		_, err = out.Write(artifact.Data)
		return err
	default:
		return fmt.Errorf("unknown account command %q", c.Operation)
	}
}

func (s AccountService) accountEntries(scope string) ([]ManagedNode, error) {
	var app App
	if ok, err := ReadJSON(filepath.Join(s.Root, "app.json"), &app); err != nil {
		return nil, err
	} else if !ok {
		return nil, errors.New("ordinary node is not initialized")
	}
	if scope == normalScope {
		return managedNodes(app)
	}
	settings, private, err := loadVPN(s.Root)
	if err != nil {
		return nil, err
	}
	p, ok := settings.Profiles[settings.ActiveCountry]
	if !ok {
		return nil, errors.New("VPNGate has no selected country")
	}
	p.Secrets = private[settings.ActiveCountry]
	return managedNodes(App{Profile: p, Inbounds: MakeInbounds(p, "vg-"+settings.ActiveCountry+"-")})
}
