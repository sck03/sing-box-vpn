package manage

import (
	"encoding/json"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"time"
)

type fixtureSubscriptionCommand struct {
	Operation, ID, Kind, ProbeURL string
	Outputs                       []string
	Expires                       time.Duration
	MaxDownloads                  *int64
	Config                        json.RawMessage
	Apply                         bool
}

// Explicit inline fixtures replace the removed shared-local source format.
func fixtureSubscriptionConfig() sub.Config {
	c := sub.DefaultConfig()
	for _, scope := range []string{normalScope, vpngateScope} {
		c.Sources = append(c.Sources, sub.Source{ID: scope, Name: scope, Kind: "inline", Format: "nodes", Content: "[]", Required: scope == normalScope})
		c.Collections = append(c.Collections, sub.Collection{ID: scope, Sources: []string{scope}, Processor: sub.Processor{Deduplicate: true}})
		for _, suffix := range []string{"sing-box", "mihomo", "provider", "base64", "uri"} {
			id := scope + "-" + suffix
			c.Outputs = append(c.Outputs, sub.Output{ID: id, Name: id, Collection: scope, Template: id})
		}
	}
	return c
}
