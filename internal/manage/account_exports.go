package manage

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/clientprofile"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type accountExportInputs struct {
	config    sub.Config
	templates map[string]sub.Template
	nodes     []sub.Node
	usage     accounting.Usage
	digest    string
}

type AccountExport struct {
	Artifact   sub.Artifact            `json:"artifact"`
	Output     sub.AccountOutputOption `json:"output"`
	Usage      accounting.Usage        `json:"-"`
	Validation sub.Validation          `json:"validation"`
}

func (s AccountService) exportInputs(ctx context.Context, command application.AccountExportRequest) (accountExportInputs, error) {
	var result accountExportInputs
	store, err := s.requireStore()
	if err != nil {
		return result, err
	}
	err = withStartupLock(ctx, s.Root, func() error {
		usage, err := store.DownloadAccount(ctx, command.ID, time.Now())
		if err != nil {
			return err
		}
		config, err := s.configSnapshot(ctx)
		if err != nil {
			return err
		}
		nodes, err := accountNodes(ctx, store, config, usage, command.Scope, command.PortOverrides)
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
		result = accountExportInputs{config: config.Subscriptions, templates: sub.AccountTemplates(config.Subscriptions, nil), nodes: processed.Nodes, usage: usage}
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
		// Local-only templates need no cache read. Any remote dependency uses the
		// same effective template as ordinary compiled outputs.
		if len(result.templates) == len(config.Subscriptions.Templates) || s.Config != nil {
			return nil
		}
		view := s.view
		if view == nil {
			view = &subscriptionView{}
		}
		view.mu.Lock()
		defer view.mu.Unlock()
		if err := view.load(s.Root); err != nil {
			// An absent or invalid publication makes remote templates unavailable.
			// Built-ins and templates with only local dependencies remain available.
			return nil
		}
		result.templates = sub.AccountTemplates(config.Subscriptions, &view.snapshot)
		return nil
	})
	return result, err
}

// Availability is a pure plan: listing profiles never renders or starts a core.
// Actual downloads and grant creation validate only the selected profiles.
func (s AccountService) OutputOptions(ctx context.Context, id, scope string, clients []string) ([]sub.AccountOutputOption, error) {
	if clients == nil {
		for _, output := range clientprofile.Outputs() {
			clients = append(clients, output.ID)
		}
	} else {
		clients = slices.Clone(clients)
	}
	result := make([]sub.AccountOutputOption, 0, len(clients))
	if len(clients) == 0 {
		return result, nil
	}
	for _, client := range clients {
		if _, ok := clientprofile.OutputFor(client); !ok {
			return nil, errors.New("unsupported account client")
		}
	}
	inputs, err := s.exportInputs(ctx, application.AccountExportRequest{ID: id, Scope: scope})
	if err != nil {
		return nil, err
	}
	for _, client := range clients {
		option, _, _ := inputs.config.PlanAccountOutput(inputs.nodes, inputs.templates, id, client)
		option.Validation = sub.Validation{State: "pending", Scope: "selection"}
		if option.Client == "sing-box" && s.Core == "" {
			option.Validation.State = "unavailable"
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result = append(result, option)
	}
	return result, nil
}

func (s AccountService) ExportDetailed(ctx context.Context, command application.AccountExportRequest) (AccountExport, error) {
	var result AccountExport
	inputs, err := s.exportInputs(ctx, command)
	if err != nil {
		return result, err
	}
	compiled, err := s.compileAccount(ctx, inputs, command.ID, command.Profile)
	result.Output, result.Usage = compiled.option, inputs.usage
	result.Artifact, result.Validation = compiled.artifact, compiled.option.Validation
	result.Artifact.Data = slices.Clone(result.Artifact.Data)
	return result, err
}

func (s AccountService) compileAccount(ctx context.Context, inputs accountExportInputs, account, client string) (accountCompilation, error) {
	template, skip, err := inputs.config.AccountTemplate(account, client, inputs.templates)
	if err != nil {
		option, _, selectErr := inputs.config.PlanAccountOutput(inputs.nodes, inputs.templates, account, client)
		return accountCompilation{option: option}, selectErr
	}
	data, err := json.Marshal(struct {
		Compiler int
		Parser   int
		Inputs   string
		Template sub.Template
		Skip     bool
	}{sub.CompilerVersion, sub.ParserVersion, inputs.digest, template, skip})
	if err != nil {
		return accountCompilation{}, err
	}
	compiled, err := s.exports.compile(ctx, sub.Digest(data), func() (accountCompilation, error) {
		option, plan, err := inputs.config.PlanAccountOutput(inputs.nodes, inputs.templates, account, client)
		value := accountCompilation{option: option}
		if err == nil {
			value.artifact, _, err = plan.Render()
			if err == nil && template.Client != "sing-box" {
				value.option.Validation, err = validateSubscriptionArtifact(ctx, s.Root, s.Core, template, value.artifact)
			}
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
	if err == nil && template.Client == "sing-box" {
		compiled.option.Validation, err = validateSubscriptionArtifact(ctx, s.Root, s.Core, template, compiled.artifact)
		if err != nil {
			compiled.option.Available = false
			compiled.option.Reason = "当前下载模板未通过配置校验"
			compiled.option.Diagnostics = append(compiled.option.Diagnostics, sub.Diagnostic{Stage: "validation", Message: err.Error()})
		}
	}
	return compiled, err
}
