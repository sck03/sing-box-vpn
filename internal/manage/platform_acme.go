package manage

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/certificates"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-acme/lego/v5/acme"
	"github.com/go-acme/lego/v5/certcrypto"
	"github.com/go-acme/lego/v5/certificate"
	"github.com/go-acme/lego/v5/challenge"
	"github.com/go-acme/lego/v5/challenge/http01"
	"github.com/go-acme/lego/v5/lego"
	"github.com/go-acme/lego/v5/registration"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

type acmeAccount struct {
	Directory    string                `json:"directory"`
	Email        string                `json:"email"`
	KeyPEM       []byte                `json:"key_pem"`
	Registration *acme.ExtendedAccount `json:"registration,omitempty"`
	key          crypto.Signer
}

func (a *acmeAccount) GetEmail() string                       { return a.Email }
func (a *acmeAccount) GetRegistration() *acme.ExtendedAccount { return a.Registration }
func (a *acmeAccount) GetPrivateKey() crypto.Signer           { return a.key }

type acmeCertificateRecord struct {
	Directory   string               `json:"directory"`
	Domain      string               `json:"domain"`
	Email       string               `json:"email"`
	AccountURL  string               `json:"account_url"`
	Resource    certificate.Resource `json:"resource"`
	Certificate []byte               `json:"certificate"`
	PrivateKey  []byte               `json:"private_key"`
	material    certificates.Material
}

func acmeAccountPath(root, directory, email string) string {
	hash := sha256.Sum256([]byte(directory + "\x00" + email))
	return filepath.Join(root, "acme", "lego", "accounts", hex.EncodeToString(hash[:])+".json")
}

func acmeCertificatePath(root, id string) string {
	return filepath.Join(root, "acme", "lego", "certificates", id+".json")
}

func loadACMEAccount(root, directory, email string) (*acmeAccount, error) {
	path := acmeAccountPath(root, directory, email)
	var account acmeAccount
	found, err := ReadJSON(path, &account)
	if err != nil {
		return nil, err
	}
	if !found {
		key, err := certcrypto.GeneratePrivateKey(certcrypto.EC256)
		if err != nil {
			return nil, err
		}
		account = acmeAccount{Directory: directory, Email: email, KeyPEM: certcrypto.PEMEncode(key)}
		// Persist the key before contacting the CA so a failed registration can
		// be retried with the same account identity.
		if err = AtomicJSON(path, account); err != nil {
			return nil, err
		}
	}
	if account.Directory != directory || account.Email != email {
		return nil, errors.New("ACME account identity does not match")
	}
	account.key, err = certcrypto.ParsePEMPrivateKey(account.KeyPEM)
	return &account, err
}

func readACMERecord(root, directory string, asset CertificateAsset) (*acmeCertificateRecord, error) {
	var record acmeCertificateRecord
	found, err := ReadJSON(acmeCertificatePath(root, asset.ID), &record)
	if err != nil || !found {
		return nil, err
	}
	// A changed domain, email or CA needs a new registration/issuance.
	if record.Directory != directory || record.Domain != asset.Domain || record.Email != asset.Email {
		return nil, nil
	}
	return &record, nil
}

func readACMECertificate(root, directory string, asset CertificateAsset) (*acmeCertificateRecord, error) {
	record, err := readACMERecord(root, directory, asset)
	if err != nil || record == nil {
		return nil, err
	}
	record.material, err = certificates.Parse(record.Certificate, record.PrivateKey, asset.Domain)
	if err != nil {
		return nil, fmt.Errorf("invalid ACME certificate cache: %w", err)
	}
	// An expired cached certificate is still usable as renewal input.
	record.Resource.Certificate, record.Resource.PrivateKey = record.Certificate, record.PrivateKey
	return record, nil
}

func acmeRegistrationReady(root string, asset CertificateAsset, material certificates.Material) bool {
	if material.Leaf == nil {
		return false
	}
	record, err := readACMERecord(root, lego.DirectoryURLLetsEncrypt, asset)
	if err != nil || record == nil || record.AccountURL == "" {
		return false
	}
	// Equal bytes reuse the caller's verified pair; no second file read or
	// private-key/certificate parsing is needed for this status check.
	if !bytes.Equal(material.Certificate, record.Certificate) || !bytes.Equal(material.Key, record.PrivateKey) {
		return false
	}
	var account acmeAccount
	found, err := ReadJSON(acmeAccountPath(root, record.Directory, asset.Email), &account)
	if err != nil || !found || account.Registration == nil || account.Registration.Location != record.AccountURL {
		return false
	}
	// A successfully issued but rolled-back pair must remain pending so the
	// scheduler retries publication instead of requesting another certificate.
	return true
}

type acmeIssuerOptions struct {
	directory string
	client    *http.Client
	provider  challenge.Provider
}

// acmeTaskTransport also cancels lego's initial directory request, which is
// made by NewClient before the context-aware registration/issuance methods.
type acmeTaskTransport struct {
	task context.Context
	base http.RoundTripper
}

func (t acmeTaskTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(request.Context())
	stop := context.AfterFunc(t.task, cancel)
	if err := t.task.Err(); err != nil {
		stop()
		cancel()
		return nil, err
	}
	response, err := t.base.RoundTrip(request.Clone(ctx))
	if err != nil {
		stop()
		cancel()
		return nil, err
	}
	response.Body = &acmeTaskBody{ReadCloser: response.Body, stop: stop, cancel: cancel}
	return response, nil
}

type acmeTaskBody struct {
	io.ReadCloser
	stop   func() bool
	cancel context.CancelFunc
}

func (b *acmeTaskBody) Close() error {
	err := b.ReadCloser.Close()
	b.stop()
	b.cancel()
	return err
}

func obtainACMECertificate(ctx context.Context, root string, asset CertificateAsset, options acmeIssuerOptions) (*certificate.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !certificates.Automatic(asset) || !domain.ResourceID.MatchString(asset.ID) || !managedCertificatePair(root, asset) {
		return nil, errors.New("ACME asset paths escape the managed certificate directory")
	}
	if options.directory == "" {
		options.directory = lego.DirectoryURLLetsEncrypt
	}
	if options.client == nil {
		options.client = &http.Client{Timeout: 30 * time.Second}
	}
	httpClient := *options.client
	transport := httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	httpClient.Transport = acmeTaskTransport{task: ctx, base: transport}
	if options.provider == nil {
		options.provider = http01.NewProviderServer("", "80")
	}
	var result *certificate.Resource
	err := withNamedFileLock(filepath.Join(root, "acme", "lego"), ".issue.lock", func() error {
		previous, err := readACMECertificate(root, options.directory, asset)
		if err != nil {
			return err
		}
		account, err := loadACMEAccount(root, options.directory, asset.Email)
		if err != nil {
			return err
		}
		sameAccount := previous != nil && account.Registration != nil && previous.AccountURL == account.Registration.Location
		if sameAccount {
			now := time.Now()
			if previous.material.ValidAt(now) == nil && previous.material.Leaf.NotAfter.Sub(now) > certificates.RenewBefore {
				result = &previous.Resource
				return nil
			}
		}
		// Explicit config avoids process-wide lego environment overrides.
		client, err := lego.NewClient(&lego.Config{
			CADirURL: options.directory, User: account, UserAgent: "sing-box-vpn",
			HTTPClient:  &httpClient,
			Certificate: lego.CertificateConfig{Timeout: 2 * time.Minute, OverallRequestLimit: certificate.DefaultOverallRequestLimit},
		})
		if err != nil {
			return err
		}
		if err = client.Challenge.SetHTTP01Provider(options.provider); err != nil {
			return err
		}
		if account.Registration == nil {
			account.Registration, err = client.Registration.Register(ctx, registration.RegisterOptions{TermsOfServiceAgreed: true})
			if err != nil {
				return err
			}
			if err = AtomicJSON(acmeAccountPath(root, options.directory, asset.Email), account); err != nil {
				return err
			}
		}
		if sameAccount {
			// Rotate the leaf key at renewal. Account identity is persisted
			// separately and ARI identifies the certificate being replaced.
			previous.Resource.PrivateKey = nil
			result, err = client.Certificate.Renew(ctx, previous.Resource, &certificate.RenewOptions{
				Bundle: true, KeyType: certcrypto.EC256, UseARICertID: true,
			})
		} else {
			result, err = client.Certificate.Obtain(ctx, certificate.ObtainRequest{
				Domains: []string{asset.Domain}, Bundle: true, KeyType: certcrypto.EC256,
			})
		}
		if err != nil {
			return err
		}
		if _, err = certificates.ValidatePEM(result.Certificate, result.PrivateKey, asset.Domain); err != nil {
			return err
		}
		// Keep a verified result even if the later service reload fails, so
		// the next task can republish it without consuming another CA order.
		return AtomicJSON(acmeCertificatePath(root, asset.ID), acmeCertificateRecord{
			Directory: options.directory, Domain: asset.Domain, Email: asset.Email,
			AccountURL: account.Registration.Location, Resource: *result,
			Certificate: result.Certificate, PrivateKey: result.PrivateKey,
		})
	})
	return result, err
}
