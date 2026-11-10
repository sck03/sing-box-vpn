package main

import (
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"

	"github.com/sck03/sing-box-vpn/internal/manage"
)

const ownerID = "sck03/sing-box-vpn:v1"

type ownerState struct {
	Owner         string `json:"owner"`
	Subnet        string `json:"subnet"`
	ForwardBefore string `json:"forward_before"`
}

func (s ownerState) validate() error {
	prefix, err := netip.ParsePrefix(s.Subnet)
	if s.Owner != ownerID || err != nil || !prefix.Addr().Is4() || !prefix.Addr().IsPrivate() || prefix.Bits() != 30 || prefix != prefix.Masked() || (s.ForwardBefore != "0" && s.ForwardBefore != "1") {
		return errors.New("invalid namespace ownership: expected owner, private IPv4 /30 network and forwarding state")
	}
	return nil
}

// Startup recovery and explicit cleanup must validate the same complete record
// before issuing any resource-management command. Read failures preserve it.
func (b *backend) recoverOwnership() error {
	var owner ownerState
	found, err := manage.ReadJSON(filepath.Join(b.cfg.DataDir, "namespace-owner.json"), &owner)
	if err != nil {
		return fmt.Errorf("read namespace ownership: %w", err)
	}
	if !found {
		return nil
	}
	if err := owner.validate(); err != nil {
		return err
	}
	b.owner, b.claimed = owner, true
	b.stop()
	return b.cleanupErr
}
