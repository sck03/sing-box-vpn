package manage

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-acme/lego/v5/acme"
	"github.com/go-acme/lego/v5/certcrypto"
	"github.com/go-acme/lego/v5/challenge/http01"
	"github.com/go-acme/lego/v5/lego"
)

type localACMEOrder struct {
	domain    string
	validated bool
	pem       []byte
}

// This CA exercises lego's real signed HTTP requests and the real HTTP-01
// listener. It never contacts a public CA or claims ownership of a real domain.
type localACMECA struct {
	t        *testing.T
	server   *httptest.Server
	key      *ecdsa.PrivateKey
	cert     *x509.Certificate
	certPEM  []byte
	httpPort string
	mu       sync.Mutex
	nonce    int
	accounts int
	thumb    string
	orders   []*localACMEOrder
	replaces int
	fail     bool
}

func newLocalACMECA(t *testing.T) *localACMECA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &localACMECA{t: t, key: key}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Local ACME test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
		SubjectKeyId: []byte("local-acme-test-ca-key"),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca.cert, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	ca.certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ca.httpPort = strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	ca.server = httptest.NewTLSServer(http.HandlerFunc(ca.serve))
	t.Cleanup(ca.server.Close)
	return ca
}

func (ca *localACMECA) signed(domain string, key crypto.PublicKey, life time.Duration) []byte {
	ca.t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		ca.t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: serial, DNSNames: []string{domain},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(life),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		AuthorityKeyId: ca.cert.SubjectKeyId,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, key, ca.key)
	if err != nil {
		ca.t.Fatal(err)
	}
	return append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), ca.certPEM...)
}

func (ca *localACMECA) serve(w http.ResponseWriter, r *http.Request) {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	ca.nonce++
	w.Header().Set("Replay-Nonce", base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("nonce-%d", ca.nonce))))
	w.Header().Set("Content-Type", "application/json")
	base := ca.server.URL
	write := func(code int, value any) {
		w.WriteHeader(code)
		if err := json.NewEncoder(w).Encode(value); err != nil {
			ca.t.Error(err)
		}
	}
	if r.URL.Path == "/directory" {
		write(200, Object{"newNonce": base + "/nonce", "newAccount": base + "/account", "newOrder": base + "/order", "renewalInfo": base + "/renewalInfo"})
		return
	}
	if r.URL.Path == "/nonce" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var jws struct{ Protected, Payload string }
	if err := json.NewDecoder(r.Body).Decode(&jws); err != nil {
		http.Error(w, "invalid JWS", 400)
		return
	}
	payload, err := base64.RawURLEncoding.DecodeString(jws.Payload)
	if err != nil {
		http.Error(w, "invalid payload", 400)
		return
	}
	switch r.URL.Path {
	case "/account":
		protected, _ := base64.RawURLEncoding.DecodeString(jws.Protected)
		var header struct{ JWK map[string]string }
		if err := json.Unmarshal(protected, &header); err != nil {
			ca.t.Error(err)
			http.Error(w, "invalid key", 400)
			return
		}
		keyJSON, _ := json.Marshal(header.JWK)
		thumb := sha256.Sum256(keyJSON)
		ca.thumb = base64.RawURLEncoding.EncodeToString(thumb[:])
		ca.accounts++
		w.Header().Set("Location", base+"/accounts/1")
		write(201, Object{"status": "valid", "orders": base + "/orders"})
		return
	case "/order":
		var request struct {
			Identifiers []struct{ Type, Value string }
			Replaces    string
		}
		if err := json.Unmarshal(payload, &request); err != nil || len(request.Identifiers) != 1 {
			http.Error(w, "invalid identifiers", 400)
			return
		}
		if request.Replaces != "" {
			ca.replaces++
		}
		ca.orders = append(ca.orders, &localACMEOrder{domain: request.Identifiers[0].Value})
		id := len(ca.orders)
		w.Header().Set("Location", fmt.Sprintf("%s/order/%d", base, id))
		write(201, ca.orderJSON(id))
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil || id < 1 || id > len(ca.orders) {
		http.NotFound(w, r)
		return
	}
	order := ca.orders[id-1]
	switch parts[0] {
	case "authorization":
		status := "pending"
		if order.validated {
			status = "valid"
		}
		write(200, Object{
			"status": status, "identifier": Object{"type": "dns", "value": order.domain},
			"challenges": []Object{{"type": "http-01", "url": fmt.Sprintf("%s/challenge/%d", base, id), "token": fmt.Sprintf("token%d", id), "status": status}},
		})
	case "challenge":
		token := fmt.Sprintf("token%d", id)
		request, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+ca.httpPort+http01.ChallengePath(token), nil)
		request.Host = order.domain
		response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
		if err != nil {
			ca.t.Error(err)
			http.Error(w, "HTTP-01 unreachable", 400)
			return
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || string(body) != token+"."+ca.thumb {
			ca.t.Error("HTTP-01 did not prove the account key")
			http.Error(w, "HTTP-01 failed", 400)
			return
		}
		order.validated = true
		write(200, Object{"type": "http-01", "status": "valid", "url": base + r.URL.Path, "token": token})
	case "order":
		write(200, ca.orderJSON(id))
	case "finalize":
		if ca.fail {
			write(400, Object{"type": "urn:ietf:params:acme:error:rejectedIdentifier", "detail": "intentional test rejection", "status": 400})
			return
		}
		var request struct{ CSR string }
		if err := json.Unmarshal(payload, &request); err != nil {
			http.Error(w, "invalid CSR", 400)
			return
		}
		der, _ := base64.RawURLEncoding.DecodeString(request.CSR)
		csr, err := x509.ParseCertificateRequest(der)
		if err != nil || csr.CheckSignature() != nil || len(csr.DNSNames) != 1 || csr.DNSNames[0] != order.domain || !order.validated {
			http.Error(w, "CSR or authorization failed", 400)
			return
		}
		order.pem = ca.signed(order.domain, csr.PublicKey, 90*24*time.Hour)
		write(200, ca.orderJSON(id))
	case "certificate":
		w.Header().Set("Content-Type", "application/pem-certificate-chain")
		w.Write(order.pem)
	default:
		http.NotFound(w, r)
	}
}

func (ca *localACMECA) orderJSON(id int) Object {
	order := ca.orders[id-1]
	base := ca.server.URL
	status := "pending"
	if order.validated {
		status = "ready"
	}
	if len(order.pem) > 0 {
		status = "valid"
	}
	return Object{
		"status": status, "identifiers": []Object{{"type": "dns", "value": order.domain}},
		"authorizations": []string{fmt.Sprintf("%s/authorization/%d", base, id)},
		"finalize":       fmt.Sprintf("%s/finalize/%d", base, id), "certificate": fmt.Sprintf("%s/certificate/%d", base, id),
	}
}

func acmeTestAsset(root string) CertificateAsset {
	return CertificateAsset{ID: "test", Provider: "acme", Domain: "acme.example.test", Email: "admin@example.test",
		Certificate: filepath.Join(root, "certificates", "test", "cert.pem"), Key: filepath.Join(root, "certificates", "test", "key.pem")}
}

func TestLegoIssueReuseRenewAndFailure(t *testing.T) {
	ca, root := newLocalACMECA(t), t.TempDir()
	asset := acmeTestAsset(root)
	directory := ca.server.URL + "/directory"
	options := acmeIssuerOptions{directory: directory, client: ca.server.Client(), provider: http01.NewProviderServer("127.0.0.1", ca.httpPort)}
	first, err := obtainACMECertificate(t.Context(), root, asset, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = validatePEM(first.Certificate, first.PrivateKey, asset.Domain); err != nil {
		t.Fatal(err)
	}
	reused, err := obtainACMECertificate(t.Context(), root, asset, options)
	if err != nil || !bytes.Equal(first.Certificate, reused.Certificate) {
		t.Fatal("fresh issuance was not reused", err)
	}
	ca.mu.Lock()
	accounts, orders := ca.accounts, len(ca.orders)
	ca.mu.Unlock()
	if accounts != 1 || orders != 1 {
		t.Fatal("repeated registration or certificate order", accounts, orders)
	}
	expireCache := func() []byte {
		t.Helper()
		record, err := readACMECertificate(root, directory, asset)
		if err != nil || record == nil {
			t.Fatal("missing renewal cache", err)
		}
		key, err := certcrypto.ParsePEMPrivateKey(record.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		record.Certificate = ca.signed(asset.Domain, key.Public(), 24*time.Hour)
		if err = AtomicJSON(acmeCertificatePath(root, asset.ID), record); err != nil {
			t.Fatal(err)
		}
		return readBytes(t, acmeCertificatePath(root, asset.ID))
	}
	expireCache()
	renewed, err := obtainACMECertificate(t.Context(), root, asset, options)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.PrivateKey, renewed.PrivateKey) {
		t.Fatal("renewal did not rotate the leaf key")
	}
	ca.mu.Lock()
	accounts, orders, replacements := ca.accounts, len(ca.orders), ca.replaces
	ca.fail = true
	ca.mu.Unlock()
	if accounts != 1 || orders != 2 || replacements != 1 {
		t.Fatal("renewal lost account identity or replacement reference", accounts, orders, replacements)
	}
	before := expireCache()
	if _, err = obtainACMECertificate(t.Context(), root, asset, options); err == nil {
		t.Fatal("accepted a rejected issuance")
	}
	if !bytes.Equal(before, readBytes(t, acmeCertificatePath(root, asset.ID))) {
		t.Fatal("failed issuance overwrote the previous cache")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+ca.httpPort)
	if err != nil {
		t.Fatal("HTTP-01 listener survived completion/failure", err)
	}
	listener.Close()
}

func TestACMERegistrationTracksPublishedPair(t *testing.T) {
	ca, root := newLocalACMECA(t), t.TempDir()
	asset := acmeTestAsset(root)
	key, err := certcrypto.GeneratePrivateKey(certcrypto.EC256)
	if err != nil {
		t.Fatal(err)
	}
	account := acmeAccount{Directory: lego.DirectoryURLLetsEncrypt, Email: asset.Email, KeyPEM: certcrypto.PEMEncode(key),
		Registration: &acme.ExtendedAccount{Location: "https://acme.example.test/account/1"}}
	record := acmeCertificateRecord{Directory: account.Directory, Domain: asset.Domain, Email: asset.Email,
		AccountURL: account.Registration.Location, Certificate: ca.signed(asset.Domain, key.Public(), 90*24*time.Hour),
		PrivateKey: certcrypto.PEMEncode(key)}
	if err = AtomicJSON(acmeAccountPath(root, account.Directory, asset.Email), account); err != nil {
		t.Fatal(err)
	}
	if err = AtomicJSON(acmeCertificatePath(root, asset.ID), record); err != nil {
		t.Fatal(err)
	}
	if acmeRegistrationReady(root, asset) {
		t.Fatal("unpublished issuance counted as registered")
	}
	if err = AtomicBytes(asset.Certificate, record.Certificate, 0600); err != nil {
		t.Fatal(err)
	}
	if err = AtomicBytes(asset.Key, record.PrivateKey, 0600); err != nil {
		t.Fatal(err)
	}
	if !acmeRegistrationReady(root, asset) {
		t.Fatal("published issuance did not complete registration")
	}
	if err = AtomicBytes(asset.Certificate, ca.signed(asset.Domain, key.Public(), 24*time.Hour), 0600); err != nil {
		t.Fatal(err)
	}
	if acmeRegistrationReady(root, asset) {
		t.Fatal("rolled-back certificate hid a pending publication")
	}
}

func TestLegoDirectoryCancellationAndPathValidation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	root := t.TempDir()
	asset := acmeTestAsset(root)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := obtainACMECertificate(ctx, root, asset, acmeIssuerOptions{directory: server.URL, client: server.Client()})
		done <- err
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatal("directory request failed before reaching the CA", err)
	case <-time.After(3 * time.Second):
		t.Fatal("directory request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("directory request ignored task cancellation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("directory request survived task cancellation")
	}
	asset.ID = "../escape"
	if _, err := obtainACMECertificate(t.Context(), root, asset, acmeIssuerOptions{}); err == nil {
		t.Fatal("accepted an unsafe asset ID")
	}
	asset = acmeTestAsset(root)
	asset.Key = filepath.Join(t.TempDir(), "outside.pem")
	if _, err := obtainACMECertificate(t.Context(), root, asset, acmeIssuerOptions{}); err == nil {
		t.Fatal("accepted an external private key path")
	}
	if _, err := os.Stat(asset.Key); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("modified an external key path", err)
	}
}
