package manage

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/sck03/sing-box-vpn/internal/certificates"
)

// CertificateStatus keeps the live registry visible when a desired change has
// not been applied. Material and actions always describe the applied asset.
func (s *PlatformService) CertificateStatus(ctx context.Context) ([]certificates.CertificateStatus, error) {
	state, err := s.Store.State(ctx)
	if err != nil {
		return nil, err
	}
	desired, err := decodePlatform(state.Desired)
	if err != nil {
		return nil, err
	}
	applied, err := decodePlatform(state.Applied)
	if err != nil {
		return nil, err
	}
	saved, live := map[string]CertificateAsset{}, map[string]CertificateAsset{}
	for _, asset := range desired.Certificates {
		saved[asset.ID] = asset
	}
	assets := map[string]CertificateAsset{}
	for id, asset := range saved {
		assets[id] = asset
	}
	for _, asset := range applied.Certificates {
		live[asset.ID], assets[asset.ID] = asset, asset
	}
	usedBy := map[string][]string{}
	addUses := func(c PlatformConfig, label string) {
		if c.Listener.Enabled {
			usedBy[c.Listener.Certificate] = append(usedBy[c.Listener.Certificate], "独立订阅（"+label+"）")
		}
		for _, node := range c.App.Nodes {
			if !node.Disabled {
				usedBy[node.TLS.Certificate] = append(usedBy[node.TLS.Certificate], node.ID+"（"+label+"）")
			}
		}
		for cc, profile := range c.VPN.Profiles {
			usedBy[profile.Certificate] = append(usedBy[profile.Certificate], "vpngate/"+cc+"（"+label+"）")
		}
	}
	addUses(applied, "已生效")
	if state.Revision != state.AppliedRevision {
		addUses(desired, "已保存")
	}
	panels, err := s.panelCertificateRefs()
	if err != nil {
		return nil, err
	}
	for label, ref := range panels {
		if ref.Certificate != "" {
			usedBy[ref.Certificate] = append(usedBy[ref.Certificate], label)
		}
	}
	ids := make([]string, 0, len(assets))
	for id := range assets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]certificates.CertificateStatus, 0, len(ids))
	for _, id := range ids {
		asset := assets[id]
		wanted, isSaved := saved[id]
		active, isApplied := live[id]
		uses := usedBy[asset.Certificate]
		if uses == nil {
			uses = []string{}
		}
		sort.Strings(uses)
		v := certificates.CertificateStatus{ID: id, Domain: asset.Domain, Provider: asset.Provider, Saved: isSaved, Applied: isApplied, Pending: !isApplied || !isSaved || wanted != active, UsedBy: uses, RenewalState: "manual"}
		material, readErr := certificates.Read(asset)
		if certificates.Automatic(asset) {
			v.RenewalState = "automatic"
			if !acmeRegistrationReady(s.Root, asset, material) {
				v.RenewalState = "registration_pending"
			}
			switch {
			case !isApplied:
				v.RenewalUnavailable = "请先应用证书配置"
			case s.NoSystem:
				v.RenewalUnavailable = "证书申请需要 Linux 部署主机"
			default:
				v.CanRenew = true
			}
		}
		if readErr == nil {
			now := time.Now()
			v.DNSNames, v.NotBefore, v.ExpiresAt = material.Leaf.DNSNames, &material.Leaf.NotBefore, &material.Leaf.NotAfter
			days := int(math.Floor(material.Leaf.NotAfter.Sub(now).Hours() / 24))
			v.DaysLeft = &days
			readErr = material.ValidAt(now)
			v.Ready = readErr == nil
		}
		if readErr != nil {
			v.Error = readErr.Error()
		}
		result = append(result, v)
	}
	return result, nil
}
