package manage

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
)

type shareAccount struct {
	accounting.Usage
	Scopes []string `json:"download_scopes"`
	Denial string   `json:"download_denial"`
}

func (s AccountService) shareAccounts(ctx context.Context, store *accounting.Store, users []accounting.Usage) ([]shareAccount, error) {
	result := make([]shareAccount, 0, len(users))
	if len(users) == 0 {
		return result, nil
	}
	config, err := s.configSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	available := map[accounting.Binding]bool{}
	for _, scope := range []string{normalScope, vpngateScope} {
		if scope == vpngateScope && config.VPN.ActiveCountry == "" {
			continue
		}
		entries, err := configuredEntries(config, scope)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.Disabled {
				available[accounting.Binding{Runtime: scope, Inbound: entry.ID, Protocol: entry.Protocol}] = true
			}
		}
	}
	ids := make([]string, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	bindings, err := store.AccountBindings(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		row := shareAccount{Usage: u, Scopes: []string{}}
		for _, b := range bindings[u.ID] {
			if available[b] && !slices.Contains(row.Scopes, b.Runtime) {
				row.Scopes = append(row.Scopes, b.Runtime)
			}
		}
		slices.Sort(row.Scopes)
		row.Denial = row.downloadDenial(accounting.Grant{})
		result = append(result, row)
	}
	return result, nil
}
func (a shareAccount) downloadDenial(g accounting.Grant) string {
	if reason := a.Usage.DownloadDenial(g); reason != "" {
		return reason
	}
	if len(a.Scopes) == 0 || (g.Scope != "" && !slices.Contains(a.Scopes, g.Scope)) {
		return "no_bound_inbound"
	}
	return ""
}
func (s AccountService) grantDenial(ctx context.Context, store *accounting.Store, u accounting.Usage, g accounting.Grant) (string, error) {
	if reason := u.DownloadDenial(g); reason != "" {
		return reason, nil
	}
	rows, err := s.shareAccounts(ctx, store, []accounting.Usage{u})
	if err != nil {
		return "", err
	}
	return rows[0].downloadDenial(g), nil
}
func (s *PlatformService) validateShare(ctx context.Context, g accounting.Grant) error {
	if g.AccountID == "" {
		return nil
	}
	u, err := s.Accounts.VisibleAccount(ctx, g.AccountID, time.Now())
	if err != nil {
		return err
	}
	reason, err := s.accountService().grantDenial(ctx, s.Accounts, u, g)
	if err != nil {
		return err
	}
	if reason == "no_bound_inbound" {
		return errors.New("所选实例没有可用入站，请先绑定已启用的节点")
	}
	if reason != "" {
		return errors.New(reason)
	}
	return nil
}
func (s *PlatformService) shareAccountPage(ctx context.Context, q application.PageQuery, archived bool) (application.Page[shareAccount], error) {
	page, err := s.Accounts.AccountPage(ctx, q, time.Now(), archived)
	result := application.Page[shareAccount]{Total: page.Total, Limit: page.Limit, Offset: page.Offset}
	if err == nil {
		result.Items, err = s.accountService().shareAccounts(ctx, s.Accounts, page.Items)
	}
	return result, err
}
