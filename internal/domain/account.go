// Package domain defines proxy identities independently of transports and storage.
package domain

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ResourceID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

var Protocols = []string{"vless", "vmess", "hysteria2", "tuic", "anytls"}

// Account is an independently metered entitlement. Several entitlements may
// belong to one subscriber without sharing credentials or quota pools.
type Account struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Disabled        bool         `json:"disabled"`
	ExpiresAt       *time.Time   `json:"expires_at"`
	QuotaBytes      *int64       `json:"quota_bytes"`
	Revision        int64        `json:"revision"`
	CreatedAt       time.Time    `json:"created_at"`
	SubscriberID    string       `json:"subscriber_id"`
	StartMode       string       `json:"start_mode"`
	StartsAt        *time.Time   `json:"starts_at"`
	DurationSeconds int64        `json:"duration_seconds"`
	Period          PeriodPolicy `json:"period"`
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
	if a.SubscriberID != "" && !ResourceID.MatchString(a.SubscriberID) {
		return errors.New("invalid subscriber ID")
	}
	if !slices.Contains([]string{"", "created", "first_use", "scheduled"}, a.StartMode) {
		return errors.New("invalid entitlement start mode")
	}
	if a.StartMode == "scheduled" && (a.StartsAt == nil || a.StartsAt.IsZero()) {
		return errors.New("scheduled entitlement requires starts_at")
	}
	if a.DurationSeconds < 0 || a.DurationSeconds > 10*366*86400 {
		return errors.New("duration must be 0–10 years in seconds")
	}
	if err := a.Period.Validate(); err != nil {
		return err
	}
	return nil
}

func (c Credential) Validate() error {
	_, err := c.Normalize()
	return err
}

// Normalize is the single boundary for effective protocol identities. UUID
// spelling must never allow two accounts to authenticate as the same user.
func (c Credential) Normalize() (Credential, error) {
	if !ResourceID.MatchString(c.ID) || !ResourceID.MatchString(c.AccountID) || !ResourceID.MatchString(c.InboundID) || !slices.Contains([]string{"normal", "vpngate"}, c.RuntimeID) || !slices.Contains(Protocols, c.Protocol) || c.Generation < 1 {
		return c, errors.New("invalid credential binding")
	}
	switch c.Protocol {
	case "vless", "vmess", "tuic":
		id, err := uuid.Parse(c.UUID)
		if err != nil || id == uuid.Nil {
			return c, errors.New("credential requires a nonzero UUID")
		}
		c.UUID = id.String()
	default:
		c.UUID = ""
	}
	switch c.Protocol {
	case "hysteria2", "tuic", "anytls":
		if len(c.Password) < 16 || len(c.Password) > 128 || strings.ContainsAny(c.Password, "\r\n\x00") {
			return c, errors.New("credential password must contain 16–128 bytes without control characters")
		}
	default:
		c.Password = ""
	}
	return c, nil
}

// Identity is defined on normalized credentials; TUIC dispatches by UUID.
func (c Credential) Identity() string {
	if c.Protocol == "anytls" || c.Protocol == "hysteria2" {
		return c.Password
	}
	return c.UUID
}
