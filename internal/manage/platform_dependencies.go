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
	accounts, grants, err := s.Accounts.InboundReferences(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	sources, collections, outputs := []string{}, []string{}, []string{}
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
	return Object{"inbound": id, "scope": scope, "accounts": accounts, "sources": sources, "outputs": outputs, "download_grants": grants}, nil
}
