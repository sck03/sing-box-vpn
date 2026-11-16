// Package application defines transport-independent commands shared by HTTP and CLI.
package application

import (
	"encoding/json"
	"time"
)

type SubscriptionCommand struct {
	Operation    string
	ID           string
	Kind         string
	Outputs      []string
	Expires      time.Duration
	MaxDownloads *int64
	Config       json.RawMessage
	Apply        bool
	ProbeURL     string
}
