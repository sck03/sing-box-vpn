package tlsconfig

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func material(t *testing.T, serial int64) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "cache.example.test"}, DNSNames: []string{"cache.example.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	raw, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})
}
func TestCacheRotationAndExpiry(t *testing.T) {
	root := t.TempDir()
	cert, key := material(t, 1)
	c := Cache{Certificate: filepath.Join(root, "cert.pem"), Key: filepath.Join(root, "key.pem"), Domain: "cache.example.test"}
	write := func(path string, raw []byte) {
		t.Helper()
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(c.Certificate, cert)
	write(c.Key, key)
	first, err := c.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	nextCert, nextKey := material(t, 2)
	write(c.Certificate, nextCert)
	c.next = time.Time{}
	partial, err := c.GetCertificate(nil)
	if err != nil || partial != first {
		t.Fatal("partial replacement lost valid pair", err)
	}
	write(c.Key, nextKey)
	c.next = time.Time{}
	next, err := c.GetCertificate(nil)
	if err != nil || next == first {
		t.Fatal("rotation not loaded", err)
	}
	leaf := *next.Leaf
	leaf.NotAfter = time.Now().Add(-time.Second)
	c.pair.Leaf = &leaf
	if expired, err := c.GetCertificate(nil); err == nil || expired != nil {
		t.Fatal("expired certificate served")
	}
	if _, err := Parse(nextCert, nextKey, "wrong.example.test"); err == nil {
		t.Fatal("hostname mismatch accepted")
	}
}
