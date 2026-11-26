package manage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/certificates"
	"net/mail"
	"os"
	"path/filepath"
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
func (s *PlatformService) renewCertificate(ctx context.Context, id string) error {
	if s.NoSystem {
		return errors.New("certificate issuance requires the Linux deployment host")
	}
	c, asset, err := s.appliedCertificate(ctx, id)
	if err != nil {
		return err
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
	files, err := s.buildApplied(ctx, c)
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
