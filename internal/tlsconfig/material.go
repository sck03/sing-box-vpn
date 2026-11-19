// Package tlsconfig validates and refreshes TLS material without business state.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"sync"
	"time"
)

func Parse(cert, key []byte, domain string) (*tls.Certificate, error) {
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, errors.New("certificate and private key do not match")
	}
	if pair.Leaf == nil {
		pair.Leaf, err = x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return nil, err
		}
	}
	if err = pair.Leaf.VerifyHostname(domain); err != nil {
		return nil, err
	}
	return &pair, nil
}

func ValidAt(pair *tls.Certificate, now time.Time) error {
	if pair == nil || pair.Leaf == nil || now.Before(pair.Leaf.NotBefore) || !now.Before(pair.Leaf.NotAfter) {
		return errors.New("certificate is not currently valid")
	}
	return nil
}

func Read(certPath, keyPath, domain string) (*tls.Certificate, error) {
	read := func(path string) ([]byte, error) {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return nil, errors.New("invalid TLS material file")
		}
		return os.ReadFile(path)
	}
	cert, err := read(certPath)
	if err != nil {
		return nil, err
	}
	key, err := read(keyPath)
	if err != nil {
		return nil, err
	}
	return Parse(cert, key, domain)
}

type Cache struct {
	Certificate, Key, Domain string
	mu                       sync.Mutex
	pair                     *tls.Certificate
	next                     time.Time
	err                      error
}

func (c *Cache) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if !now.Before(c.next) {
		pair, err := Read(c.Certificate, c.Key, c.Domain)
		if err == nil {
			err = ValidAt(pair, now)
		}
		c.err = err
		if err == nil {
			c.pair = pair
		}
		c.next = now.Add(30 * time.Second)
		if c.pair != nil && c.pair.Leaf.NotAfter.After(now) && c.pair.Leaf.NotAfter.Before(c.next) {
			c.next = c.pair.Leaf.NotAfter
		}
	}
	if ValidAt(c.pair, now) == nil {
		return c.pair, nil
	}
	if c.err != nil {
		return nil, c.err
	}
	return nil, errors.New("no valid TLS certificate")
}
