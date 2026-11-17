package manage

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/command"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func (s *PlatformService) scheduleCertificates(ctx context.Context) error {
	state, err := s.Store.State(ctx)
	if err != nil {
		return err
	}
	if state.Revision != state.AppliedRevision {
		return nil
	}
	c, err := decodePlatform(state.Applied)
	if err != nil {
		return err
	}
	for _, asset := range c.Certificates {
		if asset.Provider != "certbot" {
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
		cert, e1 := os.ReadFile(asset.Certificate)
		key, e2 := os.ReadFile(asset.Key)
		_, lineageErr := os.Stat(filepath.Join(s.Root, "acme", "config", "renewal", asset.ID+".conf"))
		if e1 == nil && e2 == nil && lineageErr == nil {
			if leaf, e := validatePEM(cert, key, asset.Domain); e == nil && time.Until(leaf.NotAfter) > 30*24*time.Hour {
				continue
			}
		}
		if _, err = s.Queue(ctx, PlatformAction{Operation: "certificate-renew", ID: asset.ID}, "scheduler"); err != nil {
			return err
		}
		return s.Store.Observe(ctx, "certificate-attempt/"+asset.ID, strconv.FormatInt(time.Now().Unix(), 10))
	}
	return nil
}

type CertificateImport struct {
	ID          string `json:"id"`
	Domain      string `json:"domain"`
	Certificate string `json:"certificate"`
	Key         string `json:"key"`
}

func validatePEM(cert, key []byte, domainName string) (*x509.Certificate, error) {
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, errors.New("certificate and private key do not match")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	if err = leaf.VerifyHostname(domainName); err != nil {
		return nil, err
	}
	if time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return nil, errors.New("certificate is not currently valid")
	}
	return leaf, nil
}
func (s *PlatformService) ImportCertificate(v CertificateImport) (CertificateAsset, error) {
	if !domain.ResourceID.MatchString(v.ID) || len(v.Certificate) > 256<<10 || len(v.Key) > 64<<10 {
		return CertificateAsset{}, errors.New("invalid certificate ID or PEM size")
	}
	if _, err := validatePEM([]byte(v.Certificate), []byte(v.Key), v.Domain); err != nil {
		return CertificateAsset{}, err
	}
	digest := sha256.Sum256([]byte(v.Certificate + v.Key))
	dir := filepath.Join(s.Root, "certificates", v.ID+"-"+hex.EncodeToString(digest[:8]))
	asset := CertificateAsset{ID: v.ID, Domain: v.Domain, Certificate: filepath.ToSlash(filepath.Join(dir, "cert.pem")), Key: filepath.ToSlash(filepath.Join(dir, "key.pem")), Provider: "import"}
	if err := AtomicBytes(asset.Key, []byte(v.Key), 0600); err != nil {
		return asset, err
	}
	return asset, AtomicBytes(asset.Certificate, []byte(v.Certificate), 0600)
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
	result := []Object{}
	for _, asset := range c.Certificates {
		v := Object{"id": asset.ID, "domain": asset.Domain, "provider": asset.Provider, "used_by": []string{}}
		v["renewal_state"] = "manual"
		if asset.Provider == "certbot" {
			v["renewal_state"] = "automatic"
			if _, err := os.Stat(filepath.Join(s.Root, "acme", "config", "renewal", asset.ID+".conf")); err != nil {
				v["renewal_state"] = "registration_pending"
			}
		}
		used := []string{}
		for _, node := range nodes {
			if node.TLS.Certificate == asset.Certificate {
				used = append(used, node.ID)
			}
		}
		for cc, p := range c.VPN.Profiles {
			if p.Certificate == asset.Certificate {
				used = append(used, "vpngate/"+cc)
			}
		}
		v["used_by"] = used
		cert, e := os.ReadFile(asset.Certificate)
		if e == nil {
			var key []byte
			key, e = os.ReadFile(asset.Key)
			if e == nil {
				var leaf *x509.Certificate
				leaf, e = validatePEM(cert, key, asset.Domain)
				if e == nil {
					v["expires_at"] = leaf.NotAfter
					v["days_left"] = int(time.Until(leaf.NotAfter).Hours() / 24)
				}
			}
		}
		if e != nil {
			v["error"] = e.Error()
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
	if asset.Provider != "certbot" || !domain.ResourceID.MatchString(asset.ID) {
		return errors.New("certificate is not owned by certbot")
	}
	address, err := mail.ParseAddress(asset.Email)
	if err != nil || address.Address != asset.Email {
		return errors.New("ACME requires a valid contact email")
	}
	base := filepath.Join(s.Root, "acme")
	_, err = command.Output(ctx, nil, "certbot", "certonly", "--standalone", "--non-interactive", "--agree-tos", "--keep-until-expiring", "--cert-name", asset.ID, "--email", asset.Email, "-d", asset.Domain, "--config-dir", filepath.Join(base, "config"), "--work-dir", filepath.Join(base, "work"), "--logs-dir", filepath.Join(base, "logs"))
	if err != nil {
		return errors.New("ACME issuance failed; check port 80 reachability and the ACME log")
	}
	cert, err := os.ReadFile(filepath.Join(base, "config", "live", asset.ID, "fullchain.pem"))
	if err != nil {
		return err
	}
	key, err := os.ReadFile(filepath.Join(base, "config", "live", asset.ID, "privkey.pem"))
	if err != nil {
		return err
	}
	if _, err = validatePEM(cert, key, asset.Domain); err != nil {
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
	if service == "sing-box" {
		for _, in := range c.App.Inbounds {
			if in["_disabled"] != true && text(object(in["tls"])["certificate_path"]) == path {
				return true
			}
		}
	}
	if service == "sing-box-vpngate" {
		p := c.VPN.Profiles[c.VPN.ActiveCountry]
		return p.Certificate == path
	}
	return false
}
