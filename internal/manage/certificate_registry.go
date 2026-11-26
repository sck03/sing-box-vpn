package manage

import (
	"context"
	"errors"
	"net/mail"
	"path/filepath"

	"github.com/sck03/sing-box-vpn/internal/certificates"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

// Register generated test material as well, without presenting it as public trust.
func initialCertificates(app App) []CertificateAsset {
	result := []CertificateAsset{}
	seen := map[string]bool{}
	for _, node := range app.Nodes {
		tls := node.TLS
		if tls.Certificate == "" || seen[tls.Certificate] {
			continue
		}
		seen[tls.Certificate] = true
		result = append(result, CertificateAsset{ID: "generated-" + node.ID, Domain: tls.ServerName, Certificate: tls.Certificate, Key: tls.Key, Provider: "generated"})
	}
	return result
}

type CertificatePreparation struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
	Email  string `json:"email"`
}

func (s *PlatformService) certificateIDAvailable(ctx context.Context, id string) error {
	if !domain.ResourceID.MatchString(id) {
		return errors.New("invalid certificate ID")
	}
	state, err := s.Store.State(ctx)
	if err != nil {
		return err
	}
	// A failed apply may remove an asset from desired while it is still live.
	// Reserve both registries so preparation cannot reuse its material directory.
	for _, raw := range [][]byte{state.Desired, state.Applied} {
		c, err := decodePlatform(raw)
		if err != nil {
			return err
		}
		for _, asset := range c.Certificates {
			if asset.ID == id {
				return errors.New("certificate ID already exists; choose a new ID before replacing bindings")
			}
		}
	}
	return nil
}

func (s *PlatformService) PrepareCertificate(ctx context.Context, value CertificatePreparation) (CertificateAsset, error) {
	if err := s.certificateIDAvailable(ctx, value.ID); err != nil {
		return CertificateAsset{}, err
	}
	if err := validateDomain(value.Domain); err != nil {
		return CertificateAsset{}, err
	}
	address, err := mail.ParseAddress(value.Email)
	if err != nil || address.Address != value.Email {
		return CertificateAsset{}, errors.New("a valid contact email is required")
	}
	dir := filepath.Join(s.Root, "certificates", value.ID)
	return CertificateAsset{ID: value.ID, Domain: value.Domain, Email: value.Email, Provider: "acme", Certificate: filepath.ToSlash(filepath.Join(dir, "cert.pem")), Key: filepath.ToSlash(filepath.Join(dir, "key.pem"))}, nil
}

func (s *PlatformService) appliedCertificate(ctx context.Context, id string) (PlatformConfig, CertificateAsset, error) {
	state, err := s.Store.AppliedState(ctx)
	if err != nil {
		return PlatformConfig{}, CertificateAsset{}, err
	}
	c, err := decodePlatform(state.Config)
	if err != nil {
		return c, CertificateAsset{}, err
	}
	for _, asset := range c.Certificates {
		if asset.ID == id && certificates.Automatic(asset) && domain.ResourceID.MatchString(id) {
			return c, asset, nil
		}
	}
	return c, CertificateAsset{}, &domain.FieldError{Field: "id", Message: "请先应用对应的 ACME 证书配置"}
}
