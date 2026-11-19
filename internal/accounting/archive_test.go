package accounting

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestComputedExpirySurvivesRenameAndStopsForwarding(t *testing.T) {
	s, _, cred := periodFixture(t, "created")
	ctx := t.Context()
	// Place the deadline close enough to exercise the authorization cache.
	if _, err := s.db.Exec(`UPDATE accounts SET duration=86400,started=unixepoch()-86400+2 WHERE id='alice'`); err != nil {
		t.Fatal(err)
	}
	u, err := s.Account(ctx, "alice")
	if err != nil || u.ExpiresAt != nil || u.EffectiveExpiresAt == nil {
		t.Fatal(u, err)
	}
	u.Name = "renamed"
	if err = s.SaveAccount(ctx, u.Account); err != nil {
		t.Fatal(err)
	}
	var unlimited bool
	if err = s.db.QueryRow(`SELECT expires IS NULL FROM accounts WHERE id='alice'`).Scan(&unlimited); err != nil || !unlimited {
		t.Fatal(unlimited, err)
	}
	m := NewMeter(s, "normal")
	defer m.Close(ctx)
	h, err := m.Open(ctx, cred.ID+".1", cred.InboundID, &closedCounter{})
	if err != nil {
		t.Fatal(err)
	}
	n, err := h.Take(1)
	if err != nil {
		t.Fatal(err)
	}
	h.Complete(n, n, true)
	time.Sleep(time.Until(*u.EffectiveExpiresAt))
	if _, err = h.Take(1); !errors.Is(err, ErrDenied) {
		t.Fatal("cached forwarding after expiry", err)
	}
	if _, err = s.Reserve(ctx, "late", u.ID, "normal", 1, *u.EffectiveExpiresAt); !errors.Is(err, ErrDenied) {
		t.Fatal("reservation at deadline", err)
	}
}

func TestArchiveCapacityRevocationAndSettlement(t *testing.T) {
	s, m, h, _ := meterFixture(t, 1000)
	ctx := t.Context()
	// Fill the active capacity without spending the test budget on 999 commits.
	if _, err := s.db.Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<999) INSERT INTO accounts(id,name,disabled,revision,created) SELECT 'extra-'||i,'extra',0,1,0 FROM n`); err != nil {
		t.Fatal(err)
	}
	replacement := domain.Account{ID: "replacement", Name: "Replacement"}
	if err := s.SaveAccount(ctx, replacement); err == nil {
		t.Fatal("capacity not enforced")
	}
	grant := Grant{ID: "g", AccountID: "alice", Clients: []string{"uri"}}
	token, err := s.CreateGrant(ctx, grant)
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Account(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	n, err := h.Take(9)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- s.ArchiveAccount(ctx, "alice", old.Revision) })
	}
	wg.Wait()
	close(results)
	var ok, conflict int
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrConflict):
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal(ok, conflict)
	}
	if _, err = h.Take(1); !errors.Is(err, ErrDenied) {
		t.Fatal("archived credential forwarded", err)
	}
	h.Complete(n, n, true)
	m.Shutdown()
	if err = m.Close(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := s.Account(ctx, "alice")
	if err != nil || u.State != "archived" || u.Charged != 9 {
		t.Fatal(u, err)
	}
	if err = s.SaveAccount(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAccount(ctx, old.Account); !errors.Is(err, ErrConflict) {
		t.Fatal("stale edit", err)
	}
	if _, _, err = s.Grant(ctx, token); !errors.Is(err, ErrDenied) {
		t.Fatal("old grant", err)
	}
	grant.ID = "new"
	if _, err = s.CreateGrant(ctx, grant); !errors.Is(err, ErrDenied) {
		t.Fatal("new grant", err)
	}
	cred := domain.Credential{ID: "fresh", AccountID: "alice", RuntimeID: "normal", InboundID: "in", Protocol: "vless", Generation: 1, UUID: "aabbccdd-1122-4333-8444-5566778899aa"}
	if err = s.Bind(ctx, cred); !errors.Is(err, ErrDenied) {
		t.Fatal("archived binding", err)
	}
	cred.AccountID = replacement.ID
	cred.ID = "credential"
	if err = s.Bind(ctx, cred); err == nil {
		t.Fatal("revoked ID reused")
	}
}
