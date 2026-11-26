package manage

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

var errOutputAccountUnavailable = errors.New("输出使用的账户权益不可用")

// Persist this small, secret-free policy alongside status. Downloads and
// delivery derive the same policy from the immutable compiled snapshot.
type subscriptionOutputPolicy struct {
	Ready      bool      `json:"ready"`
	Accounts   []string  `json:"accounts"`
	FreshUntil time.Time `json:"fresh_until,omitzero"`
}

func outputPolicy(config sub.Config, snapshot sub.Snapshot, output string) subscriptionOutputPolicy {
	policy := subscriptionOutputPolicy{Accounts: []string{}}
	_, exists := snapshot.Artifacts[output]
	contributors := snapshot.Contributors[output]
	if !exists || snapshot.Version != sub.SchemaVersion || snapshot.Revision != config.Revision || len(contributors) == 0 {
		return policy
	}
	collection := ""
	for _, item := range config.Outputs {
		if item.ID == output {
			collection = item.Collection
		}
	}
	var refs []string
	for _, item := range config.Collections {
		if item.ID == collection {
			refs = item.Sources
		}
	}
	seen := map[string]bool{}
	for _, source := range config.Sources {
		if !slices.Contains(refs, source.ID) {
			continue
		}
		contributes := slices.Contains(contributors, source.ID)
		if contributes {
			seen[source.ID] = true
		}
		if source.Kind == "account" && contributes && !slices.Contains(policy.Accounts, source.AccountID) {
			policy.Accounts = append(policy.Accounts, source.AccountID)
		}
		if source.Kind == "remote" && (contributes || source.Required) {
			last, exists := snapshot.SourceTimes[source.ID]
			if !exists || last.IsZero() {
				return policy
			}
			stale := source.MaxStaleSeconds
			if stale == 0 {
				stale = 86400
			}
			expires := last.Add(time.Duration(stale) * time.Second)
			if policy.FreshUntil.IsZero() || expires.Before(policy.FreshUntil) {
				policy.FreshUntil = expires
			}
		}
	}
	policy.Ready = len(seen) == len(contributors)
	return policy
}

func (p subscriptionOutputPolicy) check(ctx context.Context, store *accounting.Store, now time.Time) error {
	if !p.Ready {
		return errors.New("输出未生成或来源记录缺失，请重新发布")
	}
	if !p.FreshUntil.IsZero() && !now.Before(p.FreshUntil) {
		return errors.New("订阅来源已过期，请刷新来源")
	}
	for _, account := range p.Accounts {
		if store == nil {
			return errors.New("账户服务暂不可用")
		}
		if _, err := store.DownloadAccount(ctx, account, now); err != nil {
			return errOutputAccountUnavailable
		}
	}
	return nil
}
