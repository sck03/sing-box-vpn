package subscription

func fixtureConfig() Config {
	c := DefaultConfig()
	for _, scope := range []string{"normal", "vpngate"} {
		c.Sources = append(c.Sources, Source{ID: scope, Kind: "account", AccountID: scope, Scope: scope, Required: scope == "normal"})
		c.Collections = append(c.Collections, Collection{ID: scope, Sources: []string{scope}, Processor: Processor{Deduplicate: true}})
		for _, suffix := range []string{"sing-box", "mihomo", "provider", "base64", "uri"} {
			id := scope + "-" + suffix
			c.Outputs = append(c.Outputs, Output{ID: id, Collection: scope, Template: id})
		}
	}
	return c
}
