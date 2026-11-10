package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type resourceInventory struct {
	namespace, veth, filter, nat bool
	forwardingShared             bool
}

func (i resourceInventory) occupied() bool {
	return i.namespace || i.veth || i.filter || i.nat
}

// Full successful inventories distinguish absence from query failure without
// depending on localized diagnostics or ambiguous tool exit codes.
func (b *backend) inventory(ctx context.Context) (resourceInventory, error) {
	var result resourceInventory
	names, err := b.run(ctx, "ip", "netns", "list")
	if err != nil {
		return result, err
	}
	result.namespace = namespaceListed(names)
	result.forwardingShared = len(bytes.TrimSpace(names)) != 0
	links, err := b.run(ctx, "ip", "-j", "link", "show")
	if err != nil {
		return result, err
	}
	var interfaces []struct{ Ifname string }
	if err := json.Unmarshal(links, &interfaces); err != nil {
		return result, fmt.Errorf("invalid interface inventory: %w", err)
	}
	if interfaces == nil {
		return result, errors.New("interface inventory must be an array")
	}
	for _, link := range interfaces {
		if link.Ifname == "" {
			return result, errors.New("interface inventory is missing a name")
		}
		result.veth = result.veth || link.Ifname == hostVeth
	}
	filter, err := b.run(ctx, "iptables", "-w", "5", "-S")
	if err != nil {
		return result, err
	}
	result.filter = firewallChainListed(filter, "YGVG_FWD")
	nat, err := b.run(ctx, "iptables", "-w", "5", "-t", "nat", "-S")
	if err != nil {
		return result, err
	}
	result.nat = firewallChainListed(nat, "YGVG_NAT")
	// Conservatively retain forwarding when another namespace or NAT user remains.
	result.forwardingShared = result.forwardingShared || bytes.Contains(nat, []byte("MASQUERADE"))
	return result, nil
}

func namespaceListed(data []byte) bool {
	for line := range bytes.SplitSeq(data, []byte{'\n'}) {
		for name := range bytes.FieldsSeq(line) {
			if bytes.Equal(name, []byte(namespace)) {
				return true
			}
			break
		}
	}
	return false
}

// Only the operation and chain fields identify an existing chain. Rule targets,
// quoted comments and chains with a common prefix cannot establish ownership.
func firewallChainListed(data []byte, chain string) bool {
	for line := range bytes.SplitSeq(data, []byte{'\n'}) {
		operation := true
		for field := range bytes.FieldsSeq(line) {
			if operation {
				if string(field) != "-N" && string(field) != "-P" && string(field) != "-A" {
					break
				}
				operation = false
				continue
			}
			if string(field) == chain {
				return true
			}
			break
		}
	}
	return false
}
