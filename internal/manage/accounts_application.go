package manage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type AccountService struct{ Root string }

func (s AccountService) Execute(ctx context.Context, c application.AccountCommand, out io.Writer) error {
	store, err := accounting.Open(filepath.Join(s.Root, "accounts.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	switch c.Operation {
	case "list":
		accounts, err := store.Accounts(ctx)
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
		if _, err = store.Account(ctx, c.ID); err != nil {
			return err
		}
		return withStartupLock(ctx, s.Root, func() error {
			entries, err := s.accountEntries(c.Scope)
			if err != nil {
				return err
			}
			existing, err := store.Credentials(ctx, c.Scope)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if slices.ContainsFunc(existing, func(v domain.Credential) bool { return v.AccountID == c.ID && v.InboundID == entry.ID }) {
					continue
				}
				credential := domain.Credential{ID: hexSecret()[:32], AccountID: c.ID, RuntimeID: c.Scope, InboundID: entry.ID, Protocol: entry.Protocol, Generation: 1, UUID: randomUUID(), Password: randomSecret()}
				if err = store.Bind(ctx, credential); err != nil {
					return err
				}
			}
			return jsonEncoder(out).Encode(Object{"status": "saved", "runtime": c.Scope, "apply": "restart the affected managed-core instance to load new credentials"})
		})
	case "credentials":
		if !slices.Contains([]string{normalScope, vpngateScope}, c.Scope) {
			return errors.New("invalid runtime scope")
		}
		all, err := store.Credentials(ctx, c.Scope)
		if err != nil {
			return err
		}
		result := []Object{}
		for _, v := range all {
			if v.AccountID == c.ID {
				result = append(result, Object{"id": v.ID, "account_id": v.AccountID, "runtime_id": v.RuntimeID, "inbound_id": v.InboundID, "protocol": v.Protocol, "generation": v.Generation})
			}
		}
		return jsonEncoder(out).Encode(result)
	case "rotate":
		return store.Rotate(ctx, c.ID, randomUUID(), randomSecret(), c.Generation)
	case "export":
		usage, err := store.Account(ctx, c.ID)
		if err != nil {
			return err
		}
		if usage.Disabled || (usage.ExpiresAt != nil && !time.Now().Before(*usage.ExpiresAt)) {
			return accounting.ErrDenied
		}
		if !slices.Contains([]string{"sing-box", "mihomo", "uri"}, c.Client) {
			return errors.New("unsupported account client format")
		}
		var nodes []sub.Node
		err = withStartupLock(ctx, s.Root, func() error {
			var app App
			if ok, err := ReadJSON(filepath.Join(s.Root, "app.json"), &app); err != nil {
				return err
			} else if !ok {
				return errors.New("ordinary node is not initialized")
			}
			settings, private, err := loadVPN(s.Root)
			if err != nil {
				return err
			}
			local, err := collectLocalSubscriptionNodes(app, settings, private)
			if err != nil {
				return err
			}
			for _, scope := range []string{normalScope, vpngateScope} {
				if c.Scope != "" && c.Scope != scope {
					continue
				}
				entries, err := s.accountEntries(scope)
				if err != nil {
					if scope == vpngateScope && settings.ActiveCountry == "" {
						continue
					}
					return err
				}
				credentials, err := store.Credentials(ctx, scope)
				if err != nil {
					return err
				}
				for _, credential := range credentials {
					if credential.AccountID != c.ID {
						continue
					}
					if !slices.ContainsFunc(entries, func(n ManagedNode) bool { return n.ID == credential.InboundID && n.Protocol == credential.Protocol }) {
						return errors.New("credential binding no longer matches the inbound")
					}
					for _, template := range local[scope] {
						if template.Protocol != credential.Protocol {
							continue
						}
						node := template
						node.UUID = credential.UUID
						node.Password = credential.Password
						// Protocol renderers validate which secret fields are meaningful.
						if node.Protocol == "vless" || node.Protocol == "vmess" {
							node.Password = ""
						}
						if node.Protocol == "hysteria2" || node.Protocol == "anytls" {
							node.UUID = ""
						}
						nodes = append(nodes, node)
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		mode := "config"
		if c.Client == "uri" {
			mode = "nodes"
		}
		artifact, _, err := sub.Render(nodes, sub.Template{ID: "account", Version: 1, Client: c.Client, Mode: mode, Preset: "proxy"}, false)
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
