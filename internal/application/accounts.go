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
	ID            string         `json:"id"`
	Scope         string         `json:"scope,omitempty"`
	Profile       string         `json:"profile"`
	PortOverrides map[string]int `json:"port_overrides,omitempty"` // runtime/inbound ID -> TUIC port
}

type AccountPortOption struct {
	RuntimeID string   `json:"runtime_id"`
	InboundID string   `json:"inbound_id"`
	Port      int      `json:"port"`
	UDPPorts  []string `json:"udp_ports"`
}
