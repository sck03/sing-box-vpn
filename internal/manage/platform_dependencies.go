package manage

import (
	"context"
	"errors"
	"slices"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

// InboundImpact reports references without exposing authentication material.
func (s *PlatformService) InboundImpact(ctx context.Context, scope, id string) (Object, error) {
	if !slices.Contains([]string{"normal", "vpngate"}, scope) || !domain.ResourceID.MatchString(id) {
		return nil, errors.New("invalid inbound identity")
	}
	credentials, err := s.Accounts.Credentials(ctx, scope)
	if err != nil {
		return nil, err
	}
	accounts, sources, collections, outputs, grants := []string{}, []string{}, []string{}, []string{}, []string{}
	for _, credential := range credentials {
		if credential.InboundID == id && !slices.Contains(accounts, credential.AccountID) {
			accounts = append(accounts, credential.AccountID)
		}
	}
	state, err := s.Store.State(ctx)
	if err != nil {
		return nil, err
	}
	c, err := decodePlatform(state.Desired)
	if err != nil {
		return nil, err
	}
	for _, source := range c.Subscriptions.Sources {
		if source.Kind == "account" && (source.Scope == scope || source.Scope == "") && slices.Contains(accounts, source.AccountID) {
			sources = append(sources, source.ID)
		}
	}
	for _, collection := range c.Subscriptions.Collections {
		if slices.ContainsFunc(collection.Sources, func(v string) bool { return slices.Contains(sources, v) }) {
			collections = append(collections, collection.ID)
		}
	}
	for _, output := range c.Subscriptions.Outputs {
		if slices.Contains(collections, output.Collection) {
			outputs = append(outputs, output.ID)
		}
	}
	all, err := s.Accounts.Grants(ctx)
	if err != nil {
		return nil, err
	}
	for _, grant := range all {
		if !grant.Revoked && (grant.Scope == scope || grant.Scope == "") && slices.Contains(accounts, grant.AccountID) {
			grants = append(grants, grant.ID)
		}
	}
	return Object{"inbound": id, "scope": scope, "accounts": accounts, "sources": sources, "outputs": outputs, "download_grants": grants}, nil
}
