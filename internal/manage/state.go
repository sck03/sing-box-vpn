package manage

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/country"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"math/big"
	"os"
	"regexp"
	"time"
)

type Object = map[string]any

var Protocols = domain.Protocols
var Tags = map[string]string{"vless": "vless-sb", "vmess": "vmess-sb", "hysteria2": "hy2-sb", "tuic": "tuic5-sb", "anytls": "anytls-sb"}
var Labels = map[string]string{"vless": "VLESS-Reality", "vmess": "VMess-WS", "hysteria2": "Hysteria-2", "tuic": "TUIC-v5", "anytls": "AnyTLS"}
var Countries = country.Names

type Reality struct {
	Private string `json:"private_key"`
	ShortID string `json:"short_id"`
}
type Secrets struct {
	Reality   Reality `json:"reality"`
	SocksUser string  `json:"socks_user,omitempty"`
	SocksPass string  `json:"socks_pass,omitempty"`
}
type Profile struct {
	Ports         map[string]int `json:"ports"`
	Certificate   string         `json:"certificate"`
	Key           string         `json:"key"`
	RealitySNI    string         `json:"reality_sni"`
	TLSSNI        string         `json:"tls_sni"`
	VMessTLS      bool           `json:"vmess_tls"`
	WSPath        string         `json:"ws_path"`
	Secrets       Secrets        `json:"secrets,omitzero"`
	Hopping       []string       `json:"hopping,omitempty"`
	TUICPorts     []string       `json:"tuic_ports,omitempty"`
	HandshakeHost string         `json:"handshake_host,omitempty"`
	HandshakePort int            `json:"handshake_port,omitempty"`
}
type App struct {
	Name       string        `json:"name"`
	Address    string        `json:"address"`
	Nodes      []ManagedNode `json:"nodes"`
	CDNAddress string        `json:"cdn_address,omitempty"`
	Advanced   CoreSettings  `json:"advanced"`
}

// CoreSettings cannot override authenticated inbounds or VPNGate's route.
type CoreSettings struct {
	Outbounds    []Object `json:"outbounds,omitempty"`
	Route        Object   `json:"route,omitempty"`
	DNS          Object   `json:"dns,omitempty"`
	Endpoints    []Object `json:"endpoints,omitempty"`
	Services     []Object `json:"services,omitempty"`
	Log          Object   `json:"log,omitempty"`
	Experimental Object   `json:"experimental,omitempty"`
}
type VPNSettings struct {
	ActiveCountry string             `json:"active_country"`
	ProxyPort     int                `json:"proxy_port"`
	Profiles      map[string]Profile `json:"profiles"`
}

func randomBytes(size int) []byte {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand terminates on entropy failure in supported Go.
	}
	return b
}
func EnsureCertificate(certPath, keyPath, domain string) error {
	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)
	if errors.Is(certErr, os.ErrNotExist) != errors.Is(keyErr, os.ErrNotExist) {
		return errors.New("证书和私钥缺少其中一项，请修复后重试")
	}
	if errors.Is(certErr, os.ErrNotExist) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}
		serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if err != nil {
			return err
		}
		cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: domain}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{domain}, BasicConstraintsValid: true}
		der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
		if err != nil {
			return err
		}
		private, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return err
		}
		if err = AtomicBytes(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
			return err
		}
		if err = AtomicBytes(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
			os.Remove(keyPath)
			return err
		}
	}
	_, err := tls.LoadX509KeyPair(certPath, keyPath)
	return err
}

type certificateInfo = application.CertificateIdentity
type certificateCache map[string]certificateInfo

func (cache certificateCache) get(path string) (certificateInfo, error) {
	if info, exists := cache[path]; exists {
		return info, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return certificateInfo{}, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return certificateInfo{}, errors.New("证书 PEM 格式错误")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return certificateInfo{}, err
	}
	pin, hash := sha256.Sum256(cert.RawSubjectPublicKeyInfo), sha256.Sum256(cert.Raw)
	info := certificateInfo{Pin: base64.StdEncoding.EncodeToString(pin[:]), Hash: hex.EncodeToString(hash[:]), SelfSigned: bytes.Equal(cert.RawSubject, cert.RawIssuer) && cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) == nil}
	cache[path] = info
	return info, nil
}

var countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)

func validateDomain(value string) error { return domain.ValidateDomain(value) }
func object(v any) Object               { m, _ := v.(map[string]any); return m }
func text(v any) string                 { s, _ := v.(string); return s }
func number(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}
func objects(v any) []Object {
	if values, ok := v.([]Object); ok {
		return values
	}
	var out []Object
	if values, ok := v.([]any); ok {
		for _, value := range values {
			if m := object(value); m != nil {
				out = append(out, m)
			}
		}
	}
	return out
}
func stringsValue(v any) []string {
	if values, ok := v.([]string); ok {
		return values
	}
	var out []string
	if values, ok := v.([]any); ok {
		for _, value := range values {
			out = append(out, text(value))
		}
	}
	return out
}
func clone(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := Object{}
		for key, item := range value {
			out[key] = clone(item)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = clone(item)
		}
		return out
	case []Object:
		out := make([]Object, len(value))
		for i, item := range value {
			out[i] = clone(item).(Object)
		}
		return out
	case []string:
		return append([]string(nil), value...)
	default:
		return value
	}
}
