package manage

import sub "github.com/sck03/sing-box-vpn/internal/subscription"

// Preview contracts are shared with the generated panel declarations.
type ConfigurationOutputPreview struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Available   bool               `json:"available"`
	Bytes       int                `json:"bytes"`
	Diagnostics []sub.Diagnostic   `json:"diagnostics"`
	InputNodes  int                `json:"input_nodes"`
	Nodes       int                `json:"nodes"`
	Delta       sub.SelectionDelta `json:"delta"`
	Validation  sub.Validation     `json:"validation"`
}
type ConfigurationPreview struct {
	Valid          bool                         `json:"valid"`
	OrdinaryServer Object                       `json:"ordinary_server"`
	Outputs        []ConfigurationOutputPreview `json:"outputs"`
	Diagnostics    map[string][]sub.Diagnostic  `json:"diagnostics"`
}
type OutputPreviewRequest struct {
	Config PlatformConfig `json:"config"`
	ID     string         `json:"id"`
}
type OutputPreview struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	MIME        string             `json:"mime"`
	Preview     sub.ContentPreview `json:"preview"`
	Nodes       int                `json:"nodes"`
	Diagnostics []sub.Diagnostic   `json:"diagnostics"`
}
