package manage

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"time"
)

// certificateMaterial is a matched PEM pair with a parsed, hostname-checked
// leaf. Lifetime is checked separately because renewal also reads expired PEM.
type certificateMaterial struct {
	certificate []byte
	key         []byte
	pair        *tls.Certificate
	leaf        *x509.Certificate
}

func parseCertificateMaterial(cert, key []byte, domain string) (certificateMaterial, error) {
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return certificateMaterial{}, errors.New("certificate and private key do not match")
	}
	leaf := pair.Leaf
	if leaf == nil {
		// Honor Go's x509keypairleaf debug setting without parsing twice normally.
		leaf, err = x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return certificateMaterial{}, err
		}
	}
	if err = leaf.VerifyHostname(domain); err != nil {
		return certificateMaterial{}, err
	}
	return certificateMaterial{certificate: cert, key: key, leaf: leaf, pair: &pair}, nil
}

func readCertificateMaterial(asset CertificateAsset) (certificateMaterial, error) {
	cert, err := os.ReadFile(asset.Certificate)
	if err != nil {
		return certificateMaterial{}, err
	}
	key, err := os.ReadFile(asset.Key)
	if err != nil {
		return certificateMaterial{}, err
	}
	return parseCertificateMaterial(cert, key, asset.Domain)
}

func (m certificateMaterial) validateTime(now time.Time) error {
	if now.Before(m.leaf.NotBefore) || !now.Before(m.leaf.NotAfter) {
		return errors.New("certificate is not currently valid")
	}
	return nil
}

func validatePEM(cert, key []byte, domain string) (*x509.Certificate, error) {
	material, err := parseCertificateMaterial(cert, key, domain)
	if err != nil {
		return nil, err
	}
	if err = material.validateTime(time.Now()); err != nil {
		return nil, err
	}
	return material.leaf, nil
}
