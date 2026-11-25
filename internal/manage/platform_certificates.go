package manage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/certificates"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type CertificateImport struct {
	ID          string `json:"id"`
	Domain      string `json:"domain"`
	Certificate string `json:"certificate"`
	Key         string `json:"key"`
}

func (s *PlatformService) ImportCertificate(ctx context.Context, v CertificateImport) (CertificateAsset, error) {
	if err := s.certificateIDAvailable(ctx, v.ID); err != nil {
		return CertificateAsset{}, err
	}
	if err := validateDomain(v.Domain); err != nil {
		return CertificateAsset{}, err
	}
	if len(v.Certificate) > 256<<10 || len(v.Key) > 64<<10 {
		return CertificateAsset{}, errors.New("invalid certificate ID or PEM size")
	}
	if _, err := certificates.ValidatePEM([]byte(v.Certificate), []byte(v.Key), v.Domain); err != nil {
		return CertificateAsset{}, err
	}
	digest := sha256.Sum256([]byte(v.Certificate + v.Key))
	dir := filepath.Join(s.Root, "certificates", v.ID+"-"+hex.EncodeToString(digest[:8]))
	asset := CertificateAsset{ID: v.ID, Domain: v.Domain, Certificate: filepath.ToSlash(filepath.Join(dir, "cert.pem")), Key: filepath.ToSlash(filepath.Join(dir, "key.pem")), Provider: "import"}
	err := withStartupLock(ctx, s.Root, func() error {
		if err := AtomicBytes(asset.Key, []byte(v.Key), 0600); err != nil {
			return err
		}
		if err := AtomicBytes(asset.Certificate, []byte(v.Certificate), 0600); err != nil {
			return err
		}
		return AtomicBytes(filepath.Join(dir, ".managed-material"), []byte("sing-box-vpn imported certificate\n"), 0600)
	})
	return asset, err
}
func (s *PlatformService) CertificateStatus(ctx context.Context) ([]certificates.CertificateStatus, error) {
	state, err := s.Store.State(ctx)
	if err != nil {
		return nil, err
	}
	c, err := decodePlatform(state.Desired)
	if err != nil {
		return nil, err
	}
	nodes, err := managedNodes(c.App)
	if err != nil {
		return nil, err
	}
	usedBy := make(map[string][]string)
	if c.Listener.Certificate != "" {
		usedBy[c.Listener.Certificate] = append(usedBy[c.Listener.Certificate], "独立订阅（已保存）")
	}
	for _, node := range nodes {
		usedBy[node.TLS.Certificate] = append(usedBy[node.TLS.Certificate], node.ID)
	}
	for cc, profile := range c.VPN.Profiles {
		usedBy[profile.Certificate] = append(usedBy[profile.Certificate], "vpngate/"+cc)
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
	for _, uses := range usedBy {
		sort.Strings(uses)
	}
	result := make([]certificates.CertificateStatus, 0, len(c.Certificates))
	for _, asset := range c.Certificates {
		uses := usedBy[asset.Certificate]
		if uses == nil {
			uses = []string{}
		}
		v := certificates.CertificateStatus{ID: asset.ID, Domain: asset.Domain, Provider: asset.Provider, UsedBy: uses, RenewalState: "manual"}
		material, err := certificates.Read(asset)
		if certificates.Automatic(asset) {
			v.RenewalState = "automatic"
			if !acmeRegistrationReady(s.Root, asset, material) {
				v.RenewalState = "registration_pending"
			}
		}
		if err == nil {
			now := time.Now()
			err = material.ValidAt(now)
			if err == nil {
				v.Ready = true
				v.DNSNames = material.Leaf.DNSNames
				v.NotBefore = &material.Leaf.NotBefore
				v.ExpiresAt = &material.Leaf.NotAfter
				days := int(material.Leaf.NotAfter.Sub(now).Hours() / 24)
				v.DaysLeft = &days
			}
		}
		if err != nil {
			v.Error = err.Error()
		}
		result = append(result, v)
	}
	return result, nil
}
func (s *PlatformService) renewCertificate(ctx context.Context, id string) error {
	if s.NoSystem {
		return errors.New("certificate issuance requires the Linux deployment host")
	}
	state, err := s.Store.State(ctx)
	if err != nil {
		return err
	}
	c, err := decodePlatform(state.Applied)
	if err != nil {
		return err
	}
	var asset CertificateAsset
	for _, v := range c.Certificates {
		if v.ID == id {
			asset = v
		}
	}
	if !certificates.Automatic(asset) || !domain.ResourceID.MatchString(asset.ID) {
		return errors.New("certificate is not managed by ACME")
	}
	address, err := mail.ParseAddress(asset.Email)
	if err != nil || address.Address != asset.Email {
		return errors.New("ACME requires a valid contact email")
	}
	resource, err := obtainACMECertificate(ctx, s.Root, asset, acmeIssuerOptions{})
	if err != nil {
		return fmt.Errorf("ACME issuance failed; check domain resolution and port 80 reachability: %w", err)
	}
	cert, key := resource.Certificate, resource.PrivateKey
	if _, err = certificates.ValidatePEM(cert, key, asset.Domain); err != nil {
		return err
	}
	// Restored assets retain their provider and renew in their staged directory.
	// Never let a certificate task replace files outside the managed material tree.
	if !managedCertificatePair(s.Root, asset) {
		return errors.New("ACME asset paths escape the managed certificate directory")
	}
	old := fileBundle{}
	next := fileBundle{}
	for path, data := range map[string][]byte{asset.Certificate: cert, asset.Key: key} {
		rel, e := filepath.Rel(s.Root, path)
		if e != nil {
			return e
		}
		name := filepath.ToSlash(rel)
		old[name], e = os.ReadFile(path)
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		next[name] = data
	}
	active, err := s.activeServices(ctx)
	if err != nil {
		return err
	}
	if err = withStartupLock(ctx, s.Root, func() error { return publish(s.Root, next) }); err != nil {
		return err
	}
	files, err := s.build(ctx, c)
	if err == nil {
		err = s.checkRuntime(ctx, files)
	}
	if err == nil {
		for _, name := range active {
			if certificateUses(c, asset.Certificate, name) {
				if err = s.service(ctx, name, "restart"); err != nil {
					break
				}
			}
		}
	}
	if err != nil {
		recovery, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		rollback := withStartupLock(recovery, s.Root, func() error { return publish(s.Root, old) })
		for _, name := range active {
			if certificateUses(c, asset.Certificate, name) {
				rollback = errors.Join(rollback, s.service(recovery, name, "restart"))
			}
		}
		return errors.Join(err, rollback)
	}
	return nil
}

func managedCertificatePair(root string, asset CertificateAsset) bool {
	base := filepath.Join(root, "certificates")
	for _, path := range []string{asset.Certificate, asset.Key} {
		rel, err := filepath.Rel(base, path)
		if err != nil || !filepath.IsLocal(rel) {
			return false
		}
	}
	return filepath.Base(asset.Certificate) == "cert.pem" && filepath.Base(asset.Key) == "key.pem" && filepath.Dir(asset.Certificate) == filepath.Dir(asset.Key)
}

func certificateUses(c PlatformConfig, path, service string) bool {
	if service == "sb-core" {
		for _, n := range c.App.Nodes {
			if !n.Disabled && n.TLS.Certificate == path {
				return true
			}
		}
	}
	if service == "sb-vpngate-core" {
		p := c.VPN.Profiles[c.VPN.ActiveCountry]
		return p.Certificate == path
	}
	return service == "sb-subscription" && c.Listener.Enabled && c.Listener.Certificate == path
}
