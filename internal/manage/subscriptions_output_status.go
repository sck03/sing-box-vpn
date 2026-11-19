package manage

import (
	"github.com/sck03/sing-box-vpn/internal/clientprofile"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

const subscriptionsOutputStatus = "subscriptions/output-status.json"

type subscriptionOutputStatus struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Client          string           `json:"client"`
	Nodes           int              `json:"nodes"`
	Available       bool             `json:"available"`
	Validation      sub.Validation   `json:"validation"`
	DiagnosticCount int              `json:"diagnostic_count"`
	Diagnostics     []sub.Diagnostic `json:"diagnostics"`
}

func outputStatus(c sub.Config, snapshot sub.Snapshot) []subscriptionOutputStatus {
	result := make([]subscriptionOutputStatus, 0, len(c.Outputs))
	clients := map[string]string{}
	for _, t := range c.Templates {
		profile, _ := clientprofile.Lookup(t.Client)
		clients[t.ID] = profile.Label
	}
	for _, output := range c.Outputs {
		_, available := snapshot.Artifacts[output.ID]
		diagnostics := snapshot.Diagnostics[output.ID]
		row := subscriptionOutputStatus{ID: output.ID, Name: output.Name, Client: clients[output.Template], Nodes: snapshot.NodeCounts[output.ID], Available: available, DiagnosticCount: len(diagnostics)}
		row.Validation = snapshot.Validations[output.ID]
		for _, d := range diagnostics {
			if len(row.Diagnostics) < 20 {
				row.Diagnostics = append(row.Diagnostics, d)
			}
		}
		result = append(result, row)
	}
	return result
}
