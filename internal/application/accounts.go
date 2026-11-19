package application

// AccountCommand carries credential operations and account export parameters.
type AccountCommand struct {
	Operation  string   `json:"operation"`
	ID         string   `json:"id,omitempty"`
	AccountIDs []string `json:"account_ids,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	Client     string   `json:"client,omitempty"`
	Generation int64    `json:"generation,omitempty"`
	InboundIDs []string `json:"inbound_ids,omitempty"`
	TUICPort   int      `json:"tuic_port,omitempty"`
	UUID       string   `json:"uuid,omitempty"`
	Password   string   `json:"password,omitempty"`
}
