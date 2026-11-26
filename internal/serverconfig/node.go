package serverconfig

import "fmt"

type Node struct {
	ID        string    `json:"id"`
	Protocol  string    `json:"protocol"`
	Port      int       `json:"port"`
	Disabled  bool      `json:"disabled"`
	UDPPorts  []string  `json:"udp_ports,omitempty"`
	Flow      string    `json:"flow,omitempty"`
	TLS       TLS       `json:"tls"`
	Transport Transport `json:"transport,omitempty"`
}

type TLS struct {
	Enabled        bool   `json:"enabled"`
	ServerName     string `json:"server_name,omitempty"`
	Certificate    string `json:"certificate,omitempty"`
	Key            string `json:"key,omitempty"`
	RealityPrivate string `json:"reality_private,omitempty"`
	RealityShortID string `json:"reality_short_id,omitempty"`
	HandshakeHost  string `json:"handshake_host,omitempty"`
	HandshakePort  int    `json:"handshake_port,omitempty"`
}

// Transport is deliberately narrower than an imported subscription transport.
type Transport struct {
	Type            string `json:"type,omitempty"`
	Path            string `json:"path,omitempty"`
	Host            string `json:"host,omitempty"`
	EarlyData       int    `json:"early_data,omitempty"`
	EarlyDataHeader string `json:"early_data_header,omitempty"`
}

type FieldError struct{ Node, Field, Message string }

func (e *FieldError) Error() string { return fmt.Sprintf("node %s.%s: %s", e.Node, e.Field, e.Message) }
