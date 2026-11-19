package manage

import (
	"context"
	"errors"
	"slices"
)

type CredentialSummary struct {
	ID         string `json:"id"`
	AccountID  string `json:"account_id"`
	RuntimeID  string `json:"runtime_id"`
	InboundID  string `json:"inbound_id"`
	Protocol   string `json:"protocol"`
	Generation int64  `json:"generation"`
}

func (s AccountService) Credentials(ctx context.Context, id, scope string) ([]CredentialSummary, error) {
	if !slices.Contains([]string{normalScope, vpngateScope}, scope) {
		return nil, errors.New("invalid runtime scope")
	}
	store, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	all, err := store.AccountCredentials(ctx, id, scope)
	if err != nil {
		return nil, err
	}
	result := []CredentialSummary{}
	for _, v := range all {
		if v.AccountID == id {
			result = append(result, CredentialSummary{v.ID, v.AccountID, v.RuntimeID, v.InboundID, v.Protocol, v.Generation})
		}
	}
	return result, nil
}

var errNoBoundInbound = errors.New("所选实例没有可用入站")
