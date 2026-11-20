package application

// AccountCommand carries credential operations only.
type AccountCommand struct {
	Operation  string   `json:"operation"`
	ID         string   `json:"id,omitempty"`
	AccountIDs []string `json:"account_ids,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	Generation int64    `json:"generation,omitempty"`
	InboundIDs []string `json:"inbound_ids,omitempty"`
	UUID       string   `json:"uuid,omitempty"`
	Password   string   `json:"password,omitempty"`
}

type AccountExportRequest struct {
	ID       string `json:"id"`
	Scope    string `json:"scope,omitempty"`
	Profile  string `json:"profile"`
	TUICPort int    `json:"tuic_port,omitempty"`
}
