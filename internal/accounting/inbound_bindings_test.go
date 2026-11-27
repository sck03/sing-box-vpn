package accounting

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestInboundBindingsRequireExplicitUnbind(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := t.Context()
	if err = store.SaveAccount(ctx, domain.Account{ID: "account", Name: "Account"}); err != nil {
		t.Fatal(err)
	}
	credential := domain.Credential{ID: "credential", AccountID: "account", RuntimeID: "normal", InboundID: "entry", Protocol: "vless", Generation: 1, UUID: "aaaaaaaa-1111-4111-8111-111111111111"}
	if err = store.BindBatch(ctx, []domain.Credential{credential}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		nodes   []application.BoundInbound
		invalid bool
	}{
		{"same identity", []application.BoundInbound{{ID: "entry", Protocol: "vless"}}, false},
		{"removed", nil, true},
		{"renamed", []application.BoundInbound{{ID: "renamed", Protocol: "vless"}}, true},
		{"changed protocol", []application.BoundInbound{{ID: "entry", Protocol: "vmess"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := application.ValidateInboundBindings(ctx, store, "normal", tc.nodes)
			if (err != nil) != tc.invalid {
				t.Fatal(err)
			}
			if tc.invalid {
				var field *domain.FieldError
				if !errors.As(err, &field) || field.Field != "app.nodes" {
					t.Fatal(err)
				}
			}
		})
	}
	if err = application.ValidateInboundBindings(ctx, store, "vpngate", nil); err != nil {
		t.Fatal(err)
	}
	if err = store.Unbind(ctx, credential.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = application.ValidateInboundBindings(ctx, store, "normal", nil); err != nil {
		t.Fatal(err)
	}
}
