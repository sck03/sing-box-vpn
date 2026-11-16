package manage

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/clientprofile"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// Query methods return values; HTTP and CLI own their response encoding.
func (s AccountService) List(ctx context.Context) ([]accounting.Usage, error) {
	store, closeStore, err := s.openStore()
	if err != nil {
		return nil, err
	}
	defer closeStore()
	return store.CurrentAccounts(ctx, time.Now())
}

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
	store, closeStore, err := s.openStore()
	if err != nil {
		return nil, err
	}
	defer closeStore()
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

func (s AccountService) Export(ctx context.Context, c application.AccountCommand) (sub.Artifact, error) {
	profile, supported := clientprofile.Lookup(c.Client)
	if !supported {
		return sub.Artifact{}, errors.New("unsupported account client format")
	}
	store, closeStore, err := s.openStore()
	if err != nil {
		return sub.Artifact{}, err
	}
	defer closeStore()
	s.Store = store
	var nodes []sub.Node
	err = withStartupLock(ctx, s.Root, func() error {
		var err error
		nodes, err = s.accountNodes(ctx, c.ID, c.Scope, c.TUICPort)
		return err
	})
	if err != nil {
		return sub.Artifact{}, err
	}
	artifact, _, err := sub.Render(nodes, sub.Template{ID: "account", Version: 1, Client: c.Client, Mode: profile.AccountMode, Preset: "proxy"}, false)
	return artifact, err
}
