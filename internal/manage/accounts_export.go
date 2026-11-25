package manage

import (
	"context"
	"errors"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// accountNodes is shared by direct downloads, account subscription sources and
// previews. Callers own the publication lock; this function never acquires it.
func (s AccountService) accountNodes(ctx context.Context, id, scope string, tuicPort int) ([]sub.Node, error) {
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
	return accountNodes(ctx, store, config, usage, scope, tuicPort)
}

// The renderer consumes one value snapshot; it never reads configuration files
// or resolves a second applied revision halfway through an export.
func accountNodes(ctx context.Context, store *accounting.Store, config PlatformConfig, usage accounting.Usage, scope string, tuicPort int) ([]sub.Node, error) {
	app, settings := config.App, config.VPN
	var nodes []sub.Node
	certificates := certificateCache{}
	for _, runtime := range []string{normalScope, vpngateScope} {
		if scope != "" && scope != runtime {
			continue
		}
		if runtime == vpngateScope && settings.ActiveCountry == "" {
			continue
		}
		entries, err := configuredEntries(config, runtime)
		if err != nil {
			return nil, err
		}
		credentials, err := store.AccountCredentials(ctx, usage.ID, runtime)
		if err != nil {
			return nil, err
		}
		profile := app.profile()
		if runtime == vpngateScope {
			profile = settings.Profiles[settings.ActiveCountry]
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
			ports := []string(nil)
			if profile.Ports["hysteria2"] == entry.Port {
				ports = profile.Hopping
			}
			node, err := entry.subscriptionNode(app.Address, usage.Name+"-"+entry.ID, runtime, ports, certificates, credential)
			if err != nil {
				return nil, err
			}
			node.ID = runtime + "-" + credential.ID
			if node.Protocol == "tuic" && tuicPort != 0 {
				ranges, err := parsePortRanges(profile.TUICPorts)
				if err != nil {
					return nil, err
				}
				if tuicPort != node.Port && (profile.Ports["tuic"] != node.Port || !portInRanges(tuicPort, ranges)) {
					return nil, errors.New("TUIC port is not configured for this inbound")
				}
				node.Port = tuicPort
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
	}
	return nodes, nil
}
