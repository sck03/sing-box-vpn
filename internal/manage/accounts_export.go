package manage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// accountNodes is shared by direct downloads, account subscription sources and
// previews. Callers own the publication lock; this function never acquires it.
func (s AccountService) accountNodes(ctx context.Context, id, scope string, ports map[string]int) ([]sub.Node, error) {
	store, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	usage, err := store.DownloadAccount(ctx, id, time.Now())
	if err != nil {
		return nil, err
	}
	config, err := s.configSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return accountNodes(ctx, store, config, usage, scope, ports)
}

type accountBinding struct {
	runtime    string
	entry      ManagedNode
	credential domain.Credential
}

func accountBindings(ctx context.Context, store *accounting.Store, config PlatformConfig, account, scope string) ([]accountBinding, error) {
	if scope != "" && scope != normalScope && scope != vpngateScope {
		return nil, errors.New("unknown runtime scope")
	}
	var bindings []accountBinding
	for _, runtime := range []string{normalScope, vpngateScope} {
		if scope != "" && scope != runtime {
			continue
		}
		if runtime == vpngateScope && config.VPN.ActiveCountry == "" {
			continue
		}
		entries, err := configuredEntries(config, runtime)
		if err != nil {
			return nil, err
		}
		credentials, err := store.AccountCredentials(ctx, account, runtime)
		if err != nil {
			return nil, err
		}
		byID := make(map[string]ManagedNode, len(entries))
		for _, entry := range entries {
			byID[entry.ID] = entry
		}
		for _, credential := range credentials {
			entry, exists := byID[credential.InboundID]
			if !exists || entry.Protocol != credential.Protocol || entry.Disabled {
				continue
			}
			bindings = append(bindings, accountBinding{runtime, entry, credential})
		}
	}
	return bindings, nil
}

// Port choices and exports use applied configuration and the same bound entries.
func (s AccountService) ExportPortOptions(ctx context.Context, id string) ([]application.AccountPortOption, error) {
	options := []application.AccountPortOption{}
	store, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	err = withStartupLock(ctx, s.Root, func() error {
		if _, err := store.DownloadAccount(ctx, id, time.Now()); err != nil {
			return err
		}
		config, err := s.configSnapshot(ctx)
		if err != nil {
			return err
		}
		bindings, err := accountBindings(ctx, store, config, id, "")
		if err != nil {
			return err
		}
		for _, binding := range bindings {
			entry := binding.entry
			if entry.Protocol == "tuic" {
				options = append(options, application.AccountPortOption{RuntimeID: binding.runtime, InboundID: entry.ID, Port: entry.Port, UDPPorts: entry.UDPPorts})
			}
		}
		return nil
	})
	return options, err
}

// The renderer consumes one value snapshot and validates every requested override.
func accountNodes(ctx context.Context, store *accounting.Store, config PlatformConfig, usage accounting.Usage, scope string, overrides map[string]int) ([]sub.Node, error) {
	bindings, err := accountBindings(ctx, store, config, usage.ID, scope)
	if err != nil {
		return nil, err
	}
	app := config.App
	var nodes []sub.Node
	certificates := certificateCache{}
	applied := 0
	for _, binding := range bindings {
		runtime, entry, credential := binding.runtime, binding.entry, binding.credential
		node, err := entry.subscriptionNode(app.Address, usage.Name+"-"+entry.ID, runtime, entry.UDPPorts, certificates, credential)
		if err != nil {
			return nil, err
		}
		node.ID = runtime + "-" + credential.ID
		if port, override := overrides[runtime+"/"+entry.ID]; override {
			if node.Protocol != "tuic" || port < 1 || port > 65535 {
				return nil, errors.New("port overrides require a bound TUIC inbound and a valid port")
			}
			ranges, err := parsePortRanges(entry.UDPPorts)
			if err != nil {
				return nil, err
			}
			if port != node.Port && !portInRanges(port, ranges) {
				return nil, fmt.Errorf("TUIC port is not configured for %s/%s", runtime, entry.ID)
			}
			node.Port = port
			applied++
		}
		if runtime == normalScope && node.Protocol == "vmess" && app.CDNAddress != "" {
			node.Server = app.CDNAddress
		}
		if err := node.Validate(); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
		if runtime == normalScope && node.Protocol == "vmess" && config.tunnelDomain != "" && config.Argo.Inbound == entry.ID {
			domain := config.tunnelDomain
			node.ID += "-argo"
			node.Name += "-Argo"
			node.Server = domain
			node.Port = 443
			node.TLS = sub.TLS{Enabled: true, ServerName: domain}
			node.Transport.Host = domain
			if err := node.Validate(); err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
		}
	}
	if applied != len(overrides) {
		return nil, errors.New("port override refers to an unavailable inbound or a different runtime scope")
	}
	return nodes, nil
}
