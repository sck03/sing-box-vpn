package manage

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Object = map[string]any

var Protocols = []string{"vless", "vmess", "hysteria2", "tuic", "anytls"}
var Tags = map[string]string{"vless": "vless-sb", "vmess": "vmess-sb", "hysteria2": "hy2-sb", "tuic": "tuic5-sb", "anytls": "anytls-sb"}
var Labels = map[string]string{"vless": "VLESS-Reality", "vmess": "VMess-WS", "hysteria2": "Hysteria-2", "tuic": "TUIC-v5", "anytls": "AnyTLS"}
var Countries = map[string]string{"JP": "日本", "KR": "韩国", "US": "美国", "TW": "台湾", "HK": "香港", "SG": "新加坡", "TH": "泰国", "VN": "越南", "DE": "德国", "GB": "英国", "CA": "加拿大"}

type Reality struct {
	Private string `json:"private_key"`
	Public  string `json:"public_key"`
	ShortID string `json:"short_id"`
}
type Secrets struct {
	UUID      map[string]string `json:"uuid"`
	Passwords map[string]string `json:"passwords"`
	Reality   Reality           `json:"reality"`
	SocksUser string            `json:"socks_user,omitempty"`
	SocksPass string            `json:"socks_pass,omitempty"`
}
type Profile struct {
	Ports         map[string]int `json:"ports"`
	Certificate   string         `json:"certificate"`
	Key           string         `json:"key"`
	RealitySNI    string         `json:"reality_sni"`
	TLSSNI        string         `json:"tls_sni"`
	VMessTLS      bool           `json:"vmess_tls"`
	SelfSigned    bool           `json:"self_signed"`
	WSPath        string         `json:"ws_path"`
	Secrets       Secrets        `json:"secrets,omitzero"`
	Country       string         `json:"country,omitempty"`
	Hopping       []string       `json:"hopping,omitempty"`
	TUICPorts     []string       `json:"tuic_ports,omitempty"`
	HandshakeHost string         `json:"handshake_host,omitempty"`
	HandshakePort int            `json:"handshake_port,omitempty"`
}
type App struct {
	Version    string              `json:"version"`
	Name       string              `json:"name"`
	Address    string              `json:"address"`
	Profile    Profile             `json:"profile"`
	Inbounds   []Object            `json:"inbounds"`
	Routing    map[string][]string `json:"routing"`
	Warp       Object              `json:"warp"`
	SocksPort  int                 `json:"socks_port"`
	CDNAddress string              `json:"cdn_address,omitempty"`
	Argo       Object              `json:"argo,omitempty"`
}
type VPNSettings struct {
	ActiveCountry string             `json:"active_country"`
	ProxyPort     int                `json:"proxy_port"`
	Profiles      map[string]Profile `json:"profiles"`
	Order         []string           `json:"profile_order,omitempty"`
}

func ReadJSON(path string, target any) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	if err != nil {
		return false, err
	}
	if len(data) > 16<<20 {
		return false, errors.New("配置文件超过 16 MiB")
	}
	return true, json.Unmarshal(data, target)
}

func JSONBytes(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	err := encoder.Encode(value)
	return out.Bytes(), err
}

func AtomicJSON(path string, value any) error {
	data, err := JSONBytes(value)
	if err != nil {
		return err
	}
	return AtomicBytes(path, data, 0600)
}

func AtomicBytes(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func randomBytes(size int) []byte {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand terminates on entropy failure in supported Go.
	}
	return b
}
func randomSecret() string { return base64.RawURLEncoding.EncodeToString(randomBytes(32)) }
func randomUUID() string {
	b := randomBytes(16)
	b[6], b[8] = (b[6]&15)|64, (b[8]&63)|128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func derivePublic(private string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(private, "="))
	if err != nil {
		return "", errors.New("旧 Reality 私钥格式错误")
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", errors.New("旧 Reality 私钥格式错误")
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), nil
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

type certificateInfo struct {
	Pin, Hash  string
	SelfSigned bool
}
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

func AvailablePort(used map[int]bool, excluded ...portRange) (int, error) {
	for i := 0; i < 200; i++ {
		value, err := rand.Int(rand.Reader, big.NewInt(40000))
		if err != nil {
			return 0, err
		}
		port := 20000 + int(value.Int64())
		if !used[port] && !portInRanges(port, excluded) && probePort(port) == nil {
			used[port] = true
			return port, nil
		}
	}
	return 0, errors.New("没有可用入口端口")
}
func probePort(port int) error {
	address := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
	tcp, err := net.Listen("tcp4", address)
	if err != nil {
		return err
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp4", address)
	if err == nil {
		udp.Close()
	}
	return err
}

var domainPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
var countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)

func validateDomain(value string) error {
	if !domainPattern.MatchString(value) {
		return errors.New("域名格式错误")
	}
	return nil
}
func object(v any) Object { m, _ := v.(map[string]any); return m }
func text(v any) string   { s, _ := v.(string); return s }
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
func ensureObject(m Object, key string) Object {
	if value := object(m[key]); value != nil {
		return value
	}
	value := Object{}
	m[key] = value
	return value
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
