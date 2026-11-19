package manage

import (
	"encoding/json"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// Older regression fixtures use inline nodes. Reconstruct their test material
// from the current configuration instead of maintaining a runtime projection.
func loadSubscriptions(root string) (sub.Config, sub.Runtime, map[string][]sub.Node, error) {
	c, r, err := loadSubscriptionState(root)
	local := map[string][]sub.Node{}
	if err != nil {
		return c, r, local, err
	}
	for _, source := range c.Sources {
		if source.Kind == "inline" && source.Format == "nodes" {
			var nodes []sub.Node
			if err := json.Unmarshal([]byte(source.Content), &nodes); err != nil {
				return c, r, local, err
			}
			local[source.ID] = nodes
		}
	}
	return c, r, local, nil
}
