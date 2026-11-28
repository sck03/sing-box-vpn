package application

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

type publicationRepository struct {
	versions []domain.CredentialPublication
	events   *[]string
	fail     string
}

func (r *publicationRepository) CredentialPublications(context.Context) ([]domain.CredentialPublication, error) {
	return slices.Clone(r.versions), nil
}
func (r *publicationRepository) CredentialsPublished(_ context.Context, scope string, revision int64) error {
	*r.events = append(*r.events, "commit:"+scope)
	if r.fail == scope {
		return errors.New("commit failed")
	}
	for i := range r.versions {
		if r.versions[i].Runtime == scope {
			r.versions[i].Published = revision
		}
	}
	return nil
}

func TestCredentialPublicationOrderingAndRetry(t *testing.T) {
	var events []string
	repository := &publicationRepository{events: &events, fail: "vpngate", versions: []domain.CredentialPublication{
		{Runtime: "normal", Revision: 2, Published: 1, Loaded: 1},
		{Runtime: "vpngate", Revision: 3, Published: 1, Loaded: 1},
	}}
	var publications [][]string
	target := CredentialPublicationTarget{
		Publish: func(_ context.Context, pending []domain.CredentialPublication) error {
			events = append(events, "publish")
			var runtimes []string
			for _, version := range pending {
				runtimes = append(runtimes, version.Runtime)
			}
			publications = append(publications, runtimes)
			return nil
		},
		Active: func(context.Context) ([]string, error) {
			events = append(events, "active")
			return []string{"normal"}, nil
		},
		AwaitLoaded: func(_ context.Context, scope string, revision int64) error {
			if scope != "normal" || revision != 2 {
				t.Fatalf("unexpected wait: %s %d", scope, revision)
			}
			events = append(events, "await:"+scope)
			return nil
		},
	}
	if err := ReconcileCredentials(t.Context(), repository, target); err == nil {
		t.Fatal("missing commit failure")
	}
	if !reflect.DeepEqual(events, []string{"publish", "commit:normal", "commit:vpngate"}) {
		t.Fatal(events)
	}
	repository.fail = ""
	events = nil
	if err := ReconcileCredentials(t.Context(), repository, target); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(publications, [][]string{{"normal", "vpngate"}, {"vpngate"}}) {
		t.Fatal(publications)
	}
	if !reflect.DeepEqual(events, []string{"publish", "commit:vpngate", "active", "await:normal"}) {
		t.Fatal(events)
	}
	// Already-published revisions on stopped cores must not trigger host work.
	events = nil
	if err := ReconcileCredentials(t.Context(), repository, target); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatal(events)
	}
}

func TestCredentialPublicationFailureDoesNotAcknowledge(t *testing.T) {
	var events []string
	repository := &publicationRepository{events: &events, versions: []domain.CredentialPublication{{Runtime: "normal", Revision: 1}}}
	failure := errors.New("validation or atomic publish failed")
	err := ReconcileCredentials(t.Context(), repository, CredentialPublicationTarget{
		Publish: func(context.Context, []domain.CredentialPublication) error { return failure },
	})
	if !errors.Is(err, failure) || repository.versions[0].Published != 0 || len(events) != 0 {
		t.Fatalf("err=%v state=%v events=%v", err, repository.versions, events)
	}
}
