// Package runtimeconfig describes the managed runtime's supported configuration
// graph without linking the core into the management process.
package runtimeconfig

import (
	"encoding/json"
	"fmt"
	"slices"
)

type Capabilities struct {
	Inbounds       []string                  `json:"inbounds"`
	Outbounds      []string                  `json:"outbounds"`
	DNS            []string                  `json:"dns"`
	Endpoints      []string                  `json:"endpoints"`
	Services       []string                  `json:"services"`
	RouteActions   []string                  `json:"route_actions"`
	OutboundFields map[string]OutboundFields `json:"outbound_fields"`
}

type OutboundFields struct {
	TLS         bool `json:"tls"`
	RequiredTLS bool `json:"required_tls"`
	UTLS        bool `json:"utls"`
}

func Supported() Capabilities {
	return Capabilities{
		Inbounds:     []string{"vless", "vmess", "hysteria2", "tuic", "anytls"},
		Outbounds:    []string{"direct", "socks", "selector", "urltest", "vless", "vmess", "hysteria2", "tuic", "anytls"},
		DNS:          []string{"tcp", "udp", "tls", "https", "local", "resolved"},
		Endpoints:    []string{"wireguard"},
		Services:     []string{"resolved"},
		RouteActions: []string{"route", "route-options", "bypass", "reject", "hijack-dns", "sniff", "resolve"},
		OutboundFields: map[string]OutboundFields{
			"vless": {TLS: true, UTLS: true}, "vmess": {TLS: true, UTLS: true},
			"hysteria2": {TLS: true, RequiredTLS: true}, "tuic": {TLS: true, RequiredTLS: true},
			"anytls": {TLS: true, RequiredTLS: true, UTLS: true},
		},
	}
}

type FieldError struct{ Field, Kind string }

func (e *FieldError) Error() string {
	return fmt.Sprintf("%s: managed runtime does not support type %q", e.Field, e.Kind)
}

// ValidateTypes rejects unsupported components before external core validation.
// The managed binary checks options and build capabilities.
func ValidateTypes(raw []byte) error {
	type component struct {
		Type string `json:"type"`
	}
	var config struct {
		Inbounds  []component `json:"inbounds"`
		Outbounds []component `json:"outbounds"`
		Endpoints []component `json:"endpoints"`
		Services  []component `json:"services"`
		DNS       struct {
			Servers []component `json:"servers"`
		} `json:"dns"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}
	caps := Supported()
	for _, group := range []struct {
		path    string
		entries []component
		allowed []string
	}{
		{"inbounds", config.Inbounds, caps.Inbounds}, {"outbounds", config.Outbounds, caps.Outbounds},
		{"endpoints", config.Endpoints, caps.Endpoints}, {"services", config.Services, caps.Services},
		{"dns.servers", config.DNS.Servers, caps.DNS},
	} {
		for i, entry := range group.entries {
			if !slices.Contains(group.allowed, entry.Type) {
				return &FieldError{Field: fmt.Sprintf("%s[%d].type", group.path, i), Kind: entry.Type}
			}
		}
	}
	return nil
}
