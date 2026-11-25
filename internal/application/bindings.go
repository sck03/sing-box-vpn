package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

type BindingEntry struct {
	ID, Protocol string
	Disabled     bool
}
type BindingRepository interface {
	RequireAccount(context.Context, string) error
	Credentials(context.Context, string) ([]domain.Credential, error)
	BindBatch(context.Context, []domain.Credential) error
}

type BindingResult struct {
	Status   string `json:"status"`
	Runtime  string `json:"runtime"`
	Accounts int    `json:"accounts"`
	Created  int    `json:"created"`
}

// BindAccounts owns the use case; transport and topology adapters supply only
// enabled-entry metadata. The repository publishes all credentials atomically.
func BindAccounts(ctx context.Context, repository BindingRepository, c AccountCommand, entries []BindingEntry) (BindingResult, error) {
	result := BindingResult{Status: "saved", Runtime: c.Scope}
	if c.Scope != "normal" && c.Scope != "vpngate" {
		return result, errors.New("invalid runtime scope")
	}
	ids := c.AccountIDs
	if len(ids) == 0 {
		ids = []string{c.ID}
	}
	if len(ids) > 100 || (len(c.AccountIDs) > 0 && c.ID != "") {
		return result, errors.New("select 1–100 account IDs, without a separate id")
	}
	if len(ids) > 1 && (c.UUID != "" || c.Password != "") {
		return result, errors.New("batch binding requires independent random credentials")
	}
	var err error
	c, err = NormalizeCredentialInput(c)
	if err != nil {
		return result, err
	}
	available := make(map[string]BindingEntry, len(entries))
	for _, entry := range entries {
		if !entry.Disabled {
			available[entry.ID] = entry
		}
	}
	if len(c.InboundIDs) == 0 {
		return result, errors.New("select at least one explicit inbound ID")
	}
	var selected []BindingEntry
	{
		seen := map[string]bool{}
		for _, id := range c.InboundIDs {
			entry, ok := available[id]
			if !ok || seen[id] {
				return result, fmt.Errorf("inbound %s is missing, disabled or duplicated", id)
			}
			seen[id] = true
			selected = append(selected, entry)
		}
	}
	existing, err := repository.Credentials(ctx, c.Scope)
	if err != nil {
		return result, err
	}
	bound := make(map[[2]string]bool, len(existing))
	for _, credential := range existing {
		bound[[2]string{credential.AccountID, credential.InboundID}] = true
	}
	seen := map[string]bool{}
	pending := []domain.Credential{}
	for _, id := range ids {
		if seen[id] {
			return result, errors.New("duplicate account ID")
		}
		seen[id] = true
		if err = repository.RequireAccount(ctx, id); err != nil {
			return result, err
		}
		count := 0
		for _, entry := range selected {
			if entry.Disabled {
				continue
			}
			count++
			if bound[[2]string{id, entry.ID}] {
				continue
			}
			var random [16]byte
			rand.Read(random[:])
			credential := domain.Credential{ID: hex.EncodeToString(random[:]), AccountID: id, RuntimeID: c.Scope, InboundID: entry.ID, Protocol: entry.Protocol, Generation: 1, UUID: uuid.NewString(), Password: rand.Text()}
			if c.UUID != "" {
				credential.UUID = c.UUID
			}
			if c.Password != "" {
				credential.Password = c.Password
			}
			pending = append(pending, credential)
		}
		if count == 0 {
			return result, errors.New("no enabled inbounds selected")
		}
	}
	if err = repository.BindBatch(ctx, pending); err != nil {
		return result, err
	}
	result.Accounts, result.Created = len(ids), len(pending)
	return result, nil
}
