package manage

import (
	"crypto/tls"
	"sync"
	"time"
)

// Refresh at most once per interval, including failed rotations. A partial PEM
// update can use the last verified pair only while its leaf is still valid.
type panelCertificateCache struct {
	mu       sync.Mutex
	asset    CertificateAsset
	material certificateMaterial
	next     time.Time
	err      error
}

func (c *panelCertificateCache) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if !now.Before(c.next) {
		material, err := readCertificateMaterial(c.asset)
		if err == nil {
			err = material.validateTime(now)
		}
		c.err = err
		if err == nil {
			c.material = material
		}
		c.next = now.Add(30 * time.Second)
		if c.material.leaf != nil && c.material.leaf.NotAfter.After(now) && c.material.leaf.NotAfter.Before(c.next) {
			c.next = c.material.leaf.NotAfter
		}
	}
	if c.material.leaf != nil {
		if err := c.material.validateTime(now); err == nil {
			return c.material.pair, nil
		} else if c.err == nil {
			return nil, err
		}
	}
	return nil, c.err
}
