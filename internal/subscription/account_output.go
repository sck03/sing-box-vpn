package subscription

import (
	"errors"
	"github.com/sck03/sing-box-vpn/internal/clientprofile"
)

type AccountOutputOption struct {
	Profile          string       `json:"profile"`
	Description      string       `json:"description"`
	Platforms        []string     `json:"platforms"`
	ImportPrefix     string       `json:"import_prefix,omitempty"`
	Client           string       `json:"client"`
	Label            string       `json:"label"`
	Extension        string       `json:"extension"`
	Available        bool         `json:"available"`
	Input            int          `json:"input_nodes"`
	Nodes            int          `json:"nodes"`
	Skipped          int          `json:"skipped"`
	Template         string       `json:"template"`
	TemplateName     string       `json:"template_name"`
	Mode             string       `json:"mode"`
	Reason           string       `json:"reason,omitempty"`
	Experimental     bool         `json:"experimental"`
	ReferenceVersion string       `json:"reference_version,omitempty"`
	Validation       Validation   `json:"validation"`
	Diagnostics      []Diagnostic `json:"diagnostics"`
}

func (config Config) PlanAccountOutput(nodes []Node, templates map[string]Template, account, client string) (AccountOutputOption, PreparedOutput, error) {
	output, ok := clientprofile.OutputFor(client)
	profile, _ := clientprofile.Lookup(output.Client)
	option := AccountOutputOption{Profile: client, Client: output.Client, Mode: output.Mode, Label: output.Label, Description: output.Description, Extension: clientprofile.Extension(output.Client), Input: len(nodes), Experimental: profile.Experimental, Diagnostics: []Diagnostic{}}
	option.ReferenceVersion = profile.TestedVersion
	metadata, _ := clientprofile.Describe(output.Client)
	option.Platforms = metadata.Platforms
	option.ImportPrefix = clientprofile.ImportPrefix(output.Client, output.Mode)
	if !ok {
		return option, PreparedOutput{}, errors.New("unsupported account client")
	}
	t, skip, err := config.AccountTemplate(account, client, templates)
	option.Template, option.TemplateName = t.ID, t.Name
	if err != nil {
		option.Reason = "下载模板尚不可用"
		return option, PreparedOutput{}, err
	}
	plan, err := PrepareOutput(nodes, t, skip)
	option.Nodes, option.Skipped, option.Available = plan.NodeCount(), len(nodes)-plan.NodeCount(), err == nil
	option.Diagnostics = plan.Diagnostics()
	if err != nil {
		option.Reason = "所选节点与客户端或模板不兼容"
	}
	return option, plan, err
}
