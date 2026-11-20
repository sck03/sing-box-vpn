package certificates

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/tlsconfig"
	"io"
	"os"
	"time"
)

// Material is a matched PEM pair with a parsed, hostname-checked
// leaf. Lifetime is checked separately because renewal also reads expired PEM.
type Material struct {
	Certificate []byte
	Key         []byte
	Pair        *tls.Certificate
	Leaf        *x509.Certificate
}

func Parse(cert, key []byte, domain string) (Material, error) {
	pair, err := tlsconfig.Parse(cert, key, domain)
	if err != nil {
		return Material{}, err
	}
	return Material{Certificate: cert, Key: key, Leaf: pair.Leaf, Pair: pair}, nil
}

func Read(asset CertificateAsset) (Material, error) {
	cert, err := readPEM(asset.Certificate, 256<<10)
	if err != nil {
		return Material{}, err
	}
	key, err := readPEM(asset.Key, 64<<10)
	if err != nil {
		return Material{}, err
	}
	return Parse(cert, key, asset.Domain)
}

func (m Material) ValidAt(now time.Time) error {
	return tlsconfig.ValidAt(m.Pair, now)
}

func ValidatePEM(cert, key []byte, domain string) (*x509.Certificate, error) {
	material, err := Parse(cert, key, domain)
	if err != nil {
		return nil, err
	}
	if err = material.ValidAt(time.Now()); err != nil {
		return nil, err
	}
	return material.Leaf, nil
}

func readPEM(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("certificate material must be a bounded regular PEM file")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("certificate material exceeds size limit")
	}
	return data, nil
}
