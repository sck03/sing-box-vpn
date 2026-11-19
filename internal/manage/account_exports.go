package manage

import (
	"context"
	"errors"
	"os"
	"slices"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/clientprofile"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type AccountClientOption struct {
	Client           string           `json:"client"`
	Label            string           `json:"label"`
	Extension        string           `json:"extension"`
	Available        bool             `json:"available"`
	Input            int              `json:"input_nodes"`
	Nodes            int              `json:"nodes"`
	Skipped          int              `json:"skipped"`
	Template         string           `json:"template"`
	TemplateName     string           `json:"template_name"`
	Mode             string           `json:"mode"`
	Reason           string           `json:"reason,omitempty"`
	Experimental     bool             `json:"experimental"`
	ReferenceVersion string           `json:"reference_version,omitempty"`
	Validation       sub.Validation   `json:"validation"`
	Diagnostics      []sub.Diagnostic `json:"diagnostics"`
}

type accountExportInputs struct {
	config    sub.Config
	templates map[string]sub.Template
	nodes     []sub.Node
	usage     accounting.Usage
}

type AccountExport struct {
	Artifact   sub.Artifact        `json:"artifact"`
	Client     AccountClientOption `json:"client"`
	Usage      accounting.Usage    `json:"-"`
	Validation sub.Validation      `json:"validation"`
}

func (s AccountService) exportInputs(ctx context.Context, command application.AccountCommand) (accountExportInputs, error) {
	var result accountExportInputs
	store, closeStore, err := s.openStore()
	if err != nil {
		return result, err
	}
	defer closeStore()
	err = withStartupLock(ctx, s.Root, func() error {
		usage, err := store.DownloadAccount(ctx, command.ID, time.Now())
		if err != nil {
			return err
		}
		config, err := s.configSnapshot(ctx)
		if err != nil {
			return err
		}
		nodes, err := accountNodes(ctx, store, config, usage, command.Scope, command.TUICPort)
		if err != nil {
			return err
		}
		if len(nodes) == 0 {
			return errNoBoundInbound
		}
		processed, err := sub.Process(nodes, sub.Processor{})
		if err != nil {
			return err
		}
		result = accountExportInputs{config: config.Subscriptions, templates: sub.EffectiveTemplates(config.Subscriptions), nodes: processed.Nodes, usage: usage}
		// Local templates require no snapshot read. Remote templates use the same
		// immutable, atomically published cache as custom output downloads.
		remote := slices.ContainsFunc(config.Subscriptions.Templates, func(t sub.Template) bool { return t.URL != "" })
		if !remote || s.Config != nil {
			return nil
		}
		view := s.view
		if view == nil {
			view = &subscriptionView{}
		}
		view.mu.Lock()
		defer view.mu.Unlock()
		if err := view.load(s.Root); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return errors.New("remote account templates have not been published")
			}
			return err
		}
		if view.snapshot.Revision != config.Subscriptions.Revision {
			return errors.New("account template publication is pending")
		}
		result.templates = view.snapshot.Templates
		return nil
	})
	return result, err
}

func (inputs accountExportInputs) selectClient(account, client string) (AccountClientOption, sub.PreparedOutput, error) {
	profile, ok := clientprofile.Lookup(client)
	option := AccountClientOption{Client: client, Label: profile.Label, Extension: clientprofile.Extension(client), Input: len(inputs.nodes), Experimental: profile.Experimental, Diagnostics: []sub.Diagnostic{}}
	option.ReferenceVersion = profile.TestedVersion
	if !ok {
		return option, sub.PreparedOutput{}, errors.New("unsupported account client")
	}
	t, skip, err := inputs.config.AccountTemplate(account, client, inputs.templates)
	option.Template, option.TemplateName, option.Mode = t.ID, t.Name, t.Mode
	if err != nil {
		option.Reason = "下载模板尚不可用"
		return option, sub.PreparedOutput{}, err
	}
	plan, err := sub.PrepareOutput(inputs.nodes, t, skip)
	option.Nodes, option.Skipped, option.Available = plan.NodeCount(), len(inputs.nodes)-plan.NodeCount(), err == nil
	option.Diagnostics = plan.Diagnostics()
	if err != nil {
		option.Reason = "所选节点与客户端或模板不兼容"
	}
	return option, plan, err
}

func (s AccountService) ClientOptions(ctx context.Context, id, scope string) ([]AccountClientOption, error) {
	inputs, err := s.exportInputs(ctx, application.AccountCommand{ID: id, Scope: scope})
	if err != nil {
		return nil, err
	}
	clients := make([]string, 0, len(clientprofile.All()))
	for client := range clientprofile.All() {
		clients = append(clients, client)
	}
	slices.Sort(clients)
	result := make([]AccountClientOption, 0, len(clients))
	for _, client := range clients {
		option, plan, selectedErr := inputs.selectClient(id, client)
		if selectedErr == nil {
			artifact, _, err := plan.Render()
			if err == nil && s.Core != "" {
				option.Validation, err = validateSubscriptionArtifact(ctx, s.Root, s.Core, sub.Template{Client: client, Mode: option.Mode}, artifact)
			}
			if err != nil {
				option.Available = false
				option.Reason = "当前下载模板未通过配置校验"
				option.Diagnostics = append(option.Diagnostics, sub.Diagnostic{Stage: "validation", Message: err.Error()})
			}
		}
		result = append(result, option)
	}
	return result, nil
}

func (s AccountService) ExportDetailed(ctx context.Context, command application.AccountCommand) (AccountExport, error) {
	var result AccountExport
	inputs, err := s.exportInputs(ctx, command)
	if err != nil {
		return result, err
	}
	option, plan, err := inputs.selectClient(command.ID, command.Client)
	result.Client, result.Usage = option, inputs.usage
	if err != nil {
		return result, err
	}
	result.Artifact, _, err = plan.Render()
	if err == nil && s.Core != "" {
		result.Validation, err = validateSubscriptionArtifact(ctx, s.Root, s.Core, sub.Template{Client: command.Client, Mode: option.Mode}, result.Artifact)
	}
	return result, err
}
