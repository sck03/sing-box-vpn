package certificates

import (
	"context"
	"sync"
	"time"
)

const RenewBefore = 30 * 24 * time.Hour

func Automatic(asset CertificateAsset) bool { return asset.Provider == "acme" }

// RenewalSource loads only the confirmed certificate registry. An unchanged
// revision may omit its assets; desired configuration is never a renewal gate.
type RenewalSource interface {
	AppliedCertificates(context.Context, int64) (int64, []CertificateAsset, error)
	RenewalAttempt(context.Context, string) (time.Time, error)
	QueueRenewal(context.Context, string, time.Time) error
	RegistrationReady(CertificateAsset, Material) bool
}

// Scheduler owns its cache and serializes scans. The zero value is ready to use.
type Scheduler struct {
	mu       sync.Mutex
	revision int64
	assets   []CertificateAsset
	next     time.Time
}

func (s *Scheduler) Run(ctx context.Context, source RenewalSource, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	revision, assets, err := source.AppliedCertificates(ctx, s.revision)
	if err != nil {
		return err
	}
	if revision != s.revision {
		s.revision, s.assets, s.next = revision, assets, time.Time{}
	}
	if now.Before(s.next) {
		return nil
	}
	for _, asset := range s.assets {
		if !Automatic(asset) {
			continue
		}
		attempt, err := source.RenewalAttempt(ctx, asset.ID)
		if err != nil {
			return err
		}
		if !attempt.IsZero() && now.Sub(attempt) < 12*time.Hour {
			continue
		}
		material, err := Read(asset)
		if err == nil && material.ValidAt(now) == nil && material.Leaf.NotAfter.Sub(now) > RenewBefore && source.RegistrationReady(asset, material) {
			continue
		}
		if err = source.QueueRenewal(ctx, asset.ID, now); err != nil {
			return err
		}
	}
	s.next = now.Add(10 * time.Minute)
	return nil
}
