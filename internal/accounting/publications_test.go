package accounting

import (
	"path/filepath"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestCredentialPublicationSurvivesFailureAndStaleAcknowledgement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err = s.SaveAccount(ctx, domain.Account{ID: "a", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	c := domain.Credential{ID: "c", AccountID: "a", RuntimeID: "normal", InboundID: "in", Protocol: "vless", Generation: 1, UUID: "first"}
	if err = s.Bind(ctx, c); err != nil {
		t.Fatal(err)
	}
	users, revision, err := s.CredentialSnapshot(ctx, "normal")
	if err != nil || revision != 1 || len(users) != 1 {
		t.Fatal(users, revision, err)
	}
	if err = s.Rotate(ctx, "c", "second", "", 1); err != nil {
		t.Fatal(err)
	}
	if err = s.CredentialsLoaded(ctx, "normal", revision); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	versions, err := s.CredentialPublications(ctx)
	if err != nil || versions[0].Revision != 2 || versions[0].Loaded != 1 || versions[0].Published != 0 {
		t.Fatal(versions, err)
	}
	if err = s.CredentialsPublished(ctx, "normal", 2); err != nil {
		t.Fatal(err)
	}
	if err = s.Unbind(ctx, "c", 2); err != nil {
		t.Fatal(err)
	}
	versions, _ = s.CredentialPublications(ctx)
	if versions[0].Revision != 3 || versions[0].Published != 2 {
		t.Fatal(versions)
	}
}

func TestBatchCredentialUniquenessRollsBackPublication(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	for _, id := range []string{"a", "b"} {
		if err = s.SaveAccount(ctx, domain.Account{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	first := domain.Credential{ID: "one", AccountID: "a", RuntimeID: "normal", InboundID: "in", Protocol: "anytls", Generation: 1, Password: "same"}
	second := first
	second.ID = "two"
	second.AccountID = "b"
	if err = s.BindBatch(ctx, []domain.Credential{first, second}); err == nil {
		t.Fatal("duplicate authentication identity accepted")
	}
	users, revision, err := s.CredentialSnapshot(ctx, "normal")
	if err != nil || len(users) != 0 || revision != 0 {
		t.Fatal(users, revision, err)
	}
}
