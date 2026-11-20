package manage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/certificates"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

func (s *PlatformService) scheduleCertificates(ctx context.Context) error {
	s.certificateMu.Lock()
	defer s.certificateMu.Unlock()
	revisions, err := s.Store.Revisions(ctx)
	if err != nil {
		return err
	}
	if revisions.Revision != revisions.AppliedRevision {
		return nil
	}
	if revisions.AppliedRevision == s.certificateRevision && time.Now().Before(s.certificateNext) {
		return nil
	}
	data, err := s.Store.AppliedConfig(ctx)
	if err != nil {
		return err
	}
	c, err := decodePlatform(data)
	if err != nil {
		return err
	}
	// Recheck at most every ten minutes while unchanged. Revision changes bypass
	// the deadline, and failed scheduling retries on the next worker tick.
	for _, asset := range c.Certificates {
		if !automaticCertificate(asset) {
			continue
		}
		last, err := s.Store.Observation(ctx, "certificate-attempt/"+asset.ID)
		if err != nil {
			return err
		}
		attempt, _ := strconv.ParseInt(last, 10, 64)
		if time.Now().Unix()-attempt < int64((12 * time.Hour).Seconds()) {
			continue
		}
		material, err := certificates.Read(asset)
		now := time.Now()
		if err == nil && material.ValidAt(now) == nil && material.Leaf.NotAfter.Sub(now) > acmeRenewBefore &&
			acmeRegistrationReady(s.Root, asset, material) {
			continue
		}
		if _, err = s.Queue(ctx, application.TaskCommand{Operation: "certificate-renew", ID: asset.ID}, "scheduler"); err != nil {
			s.certificateNext = time.Time{}
			return err
		}
		if err = s.Store.Observe(ctx, "certificate-attempt/"+asset.ID, strconv.FormatInt(time.Now().Unix(), 10)); err != nil {
			s.certificateNext = time.Time{}
			return err
		}
	}
	s.certificateRevision = revisions.AppliedRevision
	s.certificateNext = time.Now().Add(10 * time.Minute)
	return nil
}

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
func (s *PlatformService) CertificateStatus(ctx context.Context) ([]Object, error) {
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
	result := make([]Object, 0, len(c.Certificates))
	for _, asset := range c.Certificates {
		uses := usedBy[asset.Certificate]
		if uses == nil {
			uses = []string{}
		}
		v := Object{"id": asset.ID, "domain": asset.Domain, "provider": asset.Provider, "used_by": uses, "ready": false}
		material, err := certificates.Read(asset)
		v["renewal_state"] = "manual"
		if automaticCertificate(asset) {
			v["renewal_state"] = "automatic"
			if !acmeRegistrationReady(s.Root, asset, material) {
				v["renewal_state"] = "registration_pending"
			}
		}
		if err == nil {
			now := time.Now()
			err = material.ValidAt(now)
			if err == nil {
				v["ready"] = true
				v["dns_names"] = material.Leaf.DNSNames
				v["not_before"] = material.Leaf.NotBefore
				v["expires_at"] = material.Leaf.NotAfter
				v["days_left"] = int(material.Leaf.NotAfter.Sub(now).Hours() / 24)
			}
		}
		if err != nil {
			v["error"] = err.Error()
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
	if !automaticCertificate(asset) || !domain.ResourceID.MatchString(asset.ID) {
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
