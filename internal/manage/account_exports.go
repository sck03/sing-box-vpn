package manage

import (
	"context"
	"encoding/json"
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
	digest    string
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
		data, err := json.Marshal(struct {
			Account  string
			Scope    string
			Revision int64
			Nodes    []sub.Node
		}{command.ID, command.Scope, usage.Revision, processed.Nodes})
		if err != nil {
			return err
		}
		result.digest = sub.Digest(data)
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

// nil requests the catalog; a non-nil list restricts compilation before any work.
func (s AccountService) ClientOptions(ctx context.Context, id, scope string, clients []string) ([]AccountClientOption, error) {
	if clients == nil {
		for client := range clientprofile.All() {
			clients = append(clients, client)
		}
	} else {
		clients = slices.Clone(clients)
	}
	slices.Sort(clients)
	clients = slices.Compact(clients)
	result := make([]AccountClientOption, 0, len(clients))
	if len(clients) == 0 {
		return result, nil
	}
	for _, client := range clients {
		if !clientprofile.SupportsAccount(client) {
			return nil, errors.New("unsupported account client")
		}
	}
	inputs, err := s.exportInputs(ctx, application.AccountCommand{ID: id, Scope: scope})
	if err != nil {
		return nil, err
	}
	for _, client := range clients {
		compiled, _ := s.compileAccount(ctx, inputs, id, client)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result = append(result, compiled.option)
	}
	return result, nil
}

func (s AccountService) ExportDetailed(ctx context.Context, command application.AccountCommand) (AccountExport, error) {
	var result AccountExport
	inputs, err := s.exportInputs(ctx, command)
	if err != nil {
		return result, err
	}
	compiled, err := s.compileAccount(ctx, inputs, command.ID, command.Client)
	result.Client, result.Usage = compiled.option, inputs.usage
	result.Artifact, result.Validation = compiled.artifact, compiled.option.Validation
	result.Artifact.Data = slices.Clone(result.Artifact.Data)
	return result, err
}

func (s AccountService) compileAccount(ctx context.Context, inputs accountExportInputs, account, client string) (accountCompilation, error) {
	template, skip, err := inputs.config.AccountTemplate(account, client, inputs.templates)
	if err != nil {
		option, _, selectErr := inputs.selectClient(account, client)
		return accountCompilation{option: option}, selectErr
	}
	data, err := json.Marshal(struct {
		Compiler int
		Inputs   string
		Template sub.Template
		Skip     bool
	}{sub.CompilerVersion, inputs.digest, template, skip})
	if err != nil {
		return accountCompilation{}, err
	}
	compiled, err := s.exports.compile(ctx, sub.Digest(data), func() (accountCompilation, error) {
		option, plan, err := inputs.selectClient(account, client)
		value := accountCompilation{option: option}
		if err == nil {
			value.artifact, _, err = plan.Render()
		}
		if err != nil {
			value.option.Available = false
			if value.option.Reason == "" {
				value.option.Reason = "配置生成失败"
			}
		}
		return value, err
	})
	compiled.option.Diagnostics = slices.Clone(compiled.option.Diagnostics)
	if err == nil {
		compiled.option.Validation, err = validateSubscriptionArtifact(ctx, s.Root, s.Core, template, compiled.artifact)
		if err != nil {
			compiled.option.Available = false
			compiled.option.Reason = "当前下载模板未通过配置校验"
			compiled.option.Diagnostics = append(compiled.option.Diagnostics, sub.Diagnostic{Stage: "validation", Message: err.Error()})
		}
	}
	return compiled, err
}
