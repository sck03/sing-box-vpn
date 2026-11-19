package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/anytls"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/group"
	"github.com/sagernet/sing-box/protocol/hysteria2"
	"github.com/sagernet/sing-box/protocol/socks"
	"github.com/sagernet/sing-box/protocol/tuic"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing-box/protocol/vmess"
	"github.com/sagernet/sing-box/service/resolved"
	sjson "github.com/sagernet/sing/common/json"
	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"github.com/sck03/sing-box-vpn/internal/runtimeconfig"
)

const Version = "1.14.4+sbvpn.4"

func guarded[T any](meter *accounting.Meter, constructor inbound.ConstructorFunc[T]) inbound.ConstructorFunc[T] {
	return func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options T) (adapter.Inbound, error) {
		return constructor(ctx, &guardedRouter{Router: router, meter: meter}, logger, tag, options)
	}
}

func Context(ctx context.Context, meter *accounting.Meter) context.Context {
	registry := inbound.NewRegistry()
	inbound.Register(registry, "vless", guarded(meter, vless.NewInbound))
	inbound.Register(registry, "vmess", guarded(meter, vmess.NewInbound))
	inbound.Register(registry, "hysteria2", guarded(meter, hysteria2.NewInbound))
	inbound.Register(registry, "tuic", guarded(meter, tuic.NewInbound))
	inbound.Register(registry, "anytls", guarded(meter, anytls.NewInbound))
	outbounds := outbound.NewRegistry()
	direct.RegisterOutbound(outbounds)
	socks.RegisterOutbound(outbounds)
	group.RegisterSelector(outbounds)
	group.RegisterURLTest(outbounds)
	vless.RegisterOutbound(outbounds)
	vmess.RegisterOutbound(outbounds)
	hysteria2.RegisterOutbound(outbounds)
	tuic.RegisterOutbound(outbounds)
	anytls.RegisterOutbound(outbounds)
	dnsTransports := dns.NewTransportRegistry()
	transport.RegisterTCP(dnsTransports)
	transport.RegisterUDP(dnsTransports)
	transport.RegisterTLS(dnsTransports)
	transport.RegisterHTTPS(dnsTransports)
	local.RegisterTransport(dnsTransports)
	endpoints := endpoint.NewRegistry()
	registerEndpoints(endpoints)
	services := service.NewRegistry()
	resolved.RegisterService(services)
	resolved.RegisterTransport(dnsTransports)
	return box.Context(ctx, registry, outbounds, endpoints, dnsTransports, services, certificate.NewRegistry())
}

// BindUsers replaces every protocol user list with canonical account identities.
// Unbound listeners stay empty and cannot gain an unmetered fallback user.
func BindUsers(raw []byte, credentials []domain.Credential) ([]byte, error) {
	if err := runtimeconfig.ValidateTypes(raw); err != nil {
		return nil, err
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	var nodes []map[string]json.RawMessage
	if err := json.Unmarshal(config["inbounds"], &nodes); err != nil {
		return nil, err
	}
	if len(nodes) > 256 {
		return nil, errors.New("at most 256 managed inbounds")
	}
	tags := map[string]bool{}
	byInbound := make(map[string][]domain.Credential)
	identities := make(map[[2]string]bool, len(credentials))
	for _, credential := range credentials {
		credential, err := credential.Normalize()
		if err != nil {
			return nil, fmt.Errorf("invalid credential %s: %w", credential.ID, err)
		}
		key := [2]string{credential.InboundID, credential.Identity()}
		if identities[key] {
			return nil, fmt.Errorf("duplicate authentication identity on inbound %s", credential.InboundID)
		}
		identities[key] = true
		byInbound[credential.InboundID] = append(byInbound[credential.InboundID], credential)
	}
	for _, node := range nodes {
		var kind, tag string
		if err := json.Unmarshal(node["type"], &kind); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(node["tag"], &tag); err != nil {
			return nil, err
		}
		if !domain.ResourceID.MatchString(tag) || tags[tag] {
			return nil, errors.New("inbound requires unique stable tag")
		}
		tags[tag] = true
		switch kind {
		case "vless", "vmess", "hysteria2", "tuic", "anytls":
		default:
			return nil, fmt.Errorf("unmetered inbound type %q is not supported", kind)
		}
		users := []map[string]any{}
		flow := ""
		if value := node["_managed_flow"]; value != nil {
			if err := json.Unmarshal(value, &flow); err != nil {
				return nil, err
			}
		}
		delete(node, "_managed_flow")
		if flow != "" && (kind != "vless" || flow != "xtls-rprx-vision") {
			return nil, errors.New("invalid managed flow")
		}
		for _, c := range byInbound[tag] {
			if c.Protocol != kind {
				return nil, errors.New("credential protocol changed")
			}
			u := map[string]any{"name": c.ID + "." + strconv.FormatInt(c.Generation, 10)}
			switch kind {
			case "vless":
				u["uuid"] = c.UUID
				if flow != "" {
					u["flow"] = flow
				}
			case "vmess":
				u["uuid"] = c.UUID
			case "tuic":
				u["uuid"] = c.UUID
				u["password"] = c.Password
			default:
				u["password"] = c.Password
			}
			users = append(users, u)
		}
		data, err := json.Marshal(users)
		if err != nil {
			return nil, err
		}
		node["users"] = data
	}
	// Disabled/deleted inbounds are absent from the generated listener list.
	// Their credentials cannot authenticate because no listener accepts them.
	data, err := json.Marshal(nodes)
	if err != nil {
		return nil, err
	}
	config["inbounds"] = data
	return json.Marshal(config)
}

func New(ctx context.Context, raw []byte, credentials []domain.Credential, meter *accounting.Meter) (*box.Box, error) {
	raw, err := BindUsers(raw, credentials)
	if err != nil {
		return nil, err
	}
	ctx = Context(ctx, meter)
	options, err := sjson.UnmarshalExtendedContext[option.Options](ctx, raw)
	if err != nil {
		return nil, err
	}
	return box.New(box.Options{Options: options, Context: ctx})
}
