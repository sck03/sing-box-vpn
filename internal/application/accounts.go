package application

import (
	"github.com/sck03/sing-box-vpn/internal/domain"
)

// AccountCommand is shared by the maintenance CLI and authenticated panel.
// Configuration changes require the current account or credential revision.
type AccountCommand struct {
	Operation  string         `json:"operation"`
	ID         string         `json:"id"`
	AccountIDs []string       `json:"account_ids,omitempty"`
	Scope      string         `json:"scope"`
	Client     string         `json:"client"`
	Generation int64          `json:"generation"`
	Account    domain.Account `json:"account"`
	InboundIDs []string       `json:"inbound_ids,omitempty"`
	TUICPort   int            `json:"tuic_port,omitempty"`
	UUID       string         `json:"uuid,omitempty"`
	Password   string         `json:"password,omitempty"`
}
