// Package application defines transport-independent commands shared by HTTP and CLI.
package application

type SubscriptionCommand struct {
	Operation string `json:"operation"`
	ID        string `json:"id,omitempty"`
	ProbeURL  string `json:"probe_url,omitempty"`
}
