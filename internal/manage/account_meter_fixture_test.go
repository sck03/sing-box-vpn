package manage

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
)

// Exercise the production authentication, reservation and checkpoint path.
func consumeTestQuota(t *testing.T, service *PlatformService, id string, want int) {
	t.Helper()
	ctx := t.Context()
	credentials, err := service.Accounts.AccountCredentials(ctx, id, normalScope)
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) == 0 {
		accounts := service.accountService()
		if err := accounts.Execute(ctx, application.AccountCommand{Operation: "bind", ID: id, Scope: normalScope, InboundIDs: testInboundIDs(t, accounts)}, io.Discard); err != nil {
			t.Fatal(err)
		}
		credentials, err = service.Accounts.AccountCredentials(ctx, id, normalScope)
		if err != nil || len(credentials) == 0 {
			t.Fatal(credentials, err)
		}
	}
	meter := accounting.NewMeter(service.Accounts, normalScope)
	defer func() {
		if err := meter.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	c := credentials[0]
	handle, err := meter.Open(ctx, fmt.Sprintf("%s.%d", c.ID, c.Generation), c.InboundID, io.NopCloser(strings.NewReader("")))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	n, err := handle.Take(want)
	if n > 0 {
		handle.Complete(n, n, true)
	}
	if err != nil || n != want {
		t.Fatalf("quota consumption=%d/%d: %v", n, want, err)
	}
}
