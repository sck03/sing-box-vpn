package runtime

import (
	"github.com/sck03/sing-box-vpn/internal/domain"
	"strings"
	"testing"
)

func TestBindUsersRejectsEquivalentIdentities(t *testing.T) {
	c := domain.Credential{ID: "one", AccountID: "a", RuntimeID: "normal", InboundID: "in", Protocol: "vless", Generation: 1, UUID: "aabbccdd-1122-4333-8444-5566778899aa"}
	other := c
	other.ID, other.AccountID, other.UUID = "two", "b", strings.ToUpper(c.UUID)
	_, err := BindUsers([]byte(`{"inbounds":[{"type":"vless","tag":"in","users":[]}]}`), []domain.Credential{c, other})
	if err == nil || !strings.Contains(err.Error(), "duplicate authentication identity") {
		t.Fatal(err)
	}
}
