package manage

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"path/filepath"
	"slices"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

type AccountService struct {
	Root  string
	Store *accounting.Store
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
		if !slices.Contains([]string{normalScope, vpngateScope}, c.Scope) {
			return errors.New("invalid runtime scope")
		}
		ids := c.AccountIDs
		if len(ids) == 0 {
			ids = []string{c.ID}
		}
		if len(ids) > 100 || (len(c.AccountIDs) > 0 && c.ID != "") {
			return errors.New("select 1–100 account IDs, without a separate id")
		}
		if len(ids) > 1 && (c.UUID != "" || c.Password != "") {
			return errors.New("batch binding requires independent random credentials")
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				return errors.New("duplicate account ID")
			}
			seen[id] = true
			if _, err = store.Account(ctx, id); err != nil {
				return err
			}
		}
		return withStartupLock(ctx, s.Root, func() error {
			entries, err := s.accountEntries(c.Scope)
			if err != nil {
				return err
			}
			for _, id := range c.InboundIDs {
				if !slices.ContainsFunc(entries, func(v ManagedNode) bool { return v.ID == id && !v.Disabled }) {
					return fmt.Errorf("inbound %s is missing or disabled", id)
				}
			}
			existing, err := store.Credentials(ctx, c.Scope)
			if err != nil {
				return err
			}
			pending := []domain.Credential{}
			for _, id := range ids {
				selected := 0
				for _, entry := range entries {
					if len(c.InboundIDs) > 0 && !slices.Contains(c.InboundIDs, entry.ID) {
						continue
					}
					if entry.Disabled {
						continue
					}
					selected++
					if slices.ContainsFunc(existing, func(v domain.Credential) bool { return v.AccountID == id && v.InboundID == entry.ID }) {
						continue
					}
					credential := domain.Credential{ID: hexSecret()[:32], AccountID: id, RuntimeID: c.Scope, InboundID: entry.ID, Protocol: entry.Protocol, Generation: 1, UUID: randomUUID(), Password: randomSecret()}
					if c.UUID != "" {
						credential.UUID = c.UUID
					}
					if c.Password != "" {
						credential.Password = c.Password
					}
					pending = append(pending, credential)
				}
				if selected == 0 {
					return errors.New("no enabled inbounds selected")
				}
			}
			if err = store.BindBatch(ctx, pending); err != nil {
				return err
			}
			return jsonEncoder(out).Encode(Object{"status": "saved", "runtime": c.Scope, "accounts": len(ids), "created": len(pending)})
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
