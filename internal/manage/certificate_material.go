package manage

import (
	"crypto/tls"
	"crypto/x509"
	"github.com/sck03/sing-box-vpn/internal/tlsconfig"
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
	pair, err := tlsconfig.Parse(cert, key, domain)
	if err != nil {
		return certificateMaterial{}, err
	}
	return certificateMaterial{certificate: cert, key: key, leaf: pair.Leaf, pair: pair}, nil
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
	return tlsconfig.ValidAt(m.pair, now)
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
