package manage

import (
	"context"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/accounting"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"path/filepath"
	"time"
)

type accountExportConfigKey struct{}

// accountNodes is shared by direct downloads, account subscription sources and
// previews. Callers own the publication lock; this function never acquires it.
func (s AccountService) accountNodes(ctx context.Context, id, scope string, tuicPort int) ([]sub.Node, error) {
	store, err := accounting.Open(filepath.Join(s.Root, "control.db"))
	if err != nil {
		return nil, err
	}
	defer store.Close()
	usage, err := store.Advance(ctx, id, time.Now(), false)
	if err != nil {
		return nil, err
	}
	if usage.Disabled || (usage.ExpiresAt != nil && !time.Now().Before(*usage.ExpiresAt)) {
		return nil, accounting.ErrDenied
	}
	var app App
	if _, err = ReadJSON(filepath.Join(s.Root, "app.json"), &app); err != nil {
		return nil, err
	}
	settings, _, err := loadVPN(s.Root)
	if err != nil {
		return nil, err
	}
	var private map[string]Secrets
	if config, ok := ctx.Value(accountExportConfigKey{}).(PlatformConfig); ok {
		app, settings, private = config.App, config.VPN, config.VPNSecrets
	}
	var nodes []sub.Node
	for _, runtime := range []string{normalScope, vpngateScope} {
		if scope != "" && scope != runtime {
			continue
		}
		if runtime == vpngateScope && settings.ActiveCountry == "" {
			continue
		}
		var entries []ManagedNode
		if private != nil {
			if runtime == normalScope {
				entries, err = managedNodes(app)
			} else {
				p := settings.Profiles[settings.ActiveCountry]
				p.Secrets = private[settings.ActiveCountry]
				entries, err = managedNodes(App{Profile: p, Inbounds: MakeInbounds(p, "vg-"+settings.ActiveCountry+"-")})
			}
		} else {
			entries, err = s.accountEntries(runtime)
		}
		if err != nil {
			return nil, err
		}
		credentials, err := store.Credentials(ctx, runtime)
		if err != nil {
			return nil, err
		}
		profile := app.Profile
		if runtime == vpngateScope {
			profile = settings.Profiles[settings.ActiveCountry]
		}
		for _, credential := range credentials {
			if credential.AccountID != id {
				continue
			}
			for _, entry := range entries {
				if entry.ID != credential.InboundID || entry.Protocol != credential.Protocol || entry.Disabled {
					continue
				}
				ports := []string(nil)
				if profile.Ports["hysteria2"] == entry.Port {
					ports = profile.Hopping
				}
				node, err := entry.subscriptionNode(app.Address, usage.Name+"-"+entry.ID, runtime, ports, certificateCache{})
				if err != nil {
					return nil, err
				}
				node.UUID, node.Password = credential.UUID, credential.Password
				node.ID = runtime + "-" + credential.ID
				if node.Protocol == "vless" || node.Protocol == "vmess" {
					node.Password = ""
				}
				if node.Protocol == "hysteria2" || node.Protocol == "anytls" {
					node.UUID = ""
				}
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
				nodes = append(nodes, node)
				if runtime == normalScope && node.Protocol == "vmess" && text(app.Argo["domain"]) != "" && (text(app.Argo["inbound"]) == "" || text(app.Argo["inbound"]) == entry.ID) {
					domain := text(app.Argo["domain"])
					node.ID += "-argo"
					node.Name += "-Argo"
					node.Server = domain
					node.Port = 443
					node.TLS = sub.TLS{Enabled: true, ServerName: domain}
					node.Transport.Host = domain
					nodes = append(nodes, node)
				}
			}
		}
	}
	return nodes, nil
}
