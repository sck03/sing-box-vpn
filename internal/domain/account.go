// Package domain defines proxy identities independently of transports and storage.
package domain

import (
	"errors"
	"regexp"
	"slices"
	"time"
)

var ResourceID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

var Protocols = []string{"vless", "vmess", "hysteria2", "tuic", "anytls"}

// An account owns a single entitlement in the first schema. IDs, rather than
// names or download tokens, identify its durable usage pool.
type Account struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Disabled   bool       `json:"disabled"`
	ExpiresAt  *time.Time `json:"expires_at"`
	QuotaBytes *int64     `json:"quota_bytes"`
	Revision   int64      `json:"revision"`
	CreatedAt  time.Time  `json:"created_at"`
}

type Credential struct {
	ID         string `json:"id"`
	AccountID  string `json:"account_id"`
	RuntimeID  string `json:"runtime_id"`
	InboundID  string `json:"inbound_id"`
	Protocol   string `json:"protocol"`
	Generation int64  `json:"generation"`
	UUID       string `json:"uuid,omitempty"`
	Password   string `json:"password,omitempty"`
}

func (a Account) Validate() error {
	if !ResourceID.MatchString(a.ID) || len(a.Name) == 0 || len(a.Name) > 128 {
		return errors.New("account requires a stable ID and a name of 1–128 bytes")
	}
	if a.QuotaBytes != nil && (*a.QuotaBytes < 0 || *a.QuotaBytes > 1<<60) {
		return errors.New("quota must be null (unlimited) or 0–2^60 bytes")
	}
	if a.ExpiresAt != nil && a.ExpiresAt.IsZero() {
		return errors.New("expiry must be null or a valid timestamp")
	}
	return nil
}

func (c Credential) Validate() error {
	if !ResourceID.MatchString(c.ID) || !ResourceID.MatchString(c.AccountID) || !ResourceID.MatchString(c.InboundID) || !slices.Contains([]string{"normal", "vpngate"}, c.RuntimeID) || !slices.Contains(Protocols, c.Protocol) || c.Generation < 1 {
		return errors.New("invalid credential binding")
	}
	return nil
}
