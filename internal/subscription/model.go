// Package subscription implements bounded, deterministic subscription conversion.
// It has no dependency on the host's proxy services or management commands.
package subscription

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

const (
	SchemaVersion = 6
	// ParserVersion invalidates fetched node caches, including conditional GETs.
	ParserVersion = 3
	// CompilerVersion invalidates rendered artifacts and account export plans.
	CompilerVersion = 8
	MaxNodes        = 4096
	MaxBody         = 8 << 20
	MaxObjects      = 128
)

type TLS struct {
	Enabled         bool     `json:"enabled,omitempty"`
	ServerName      string   `json:"server_name,omitempty"`
	Insecure        bool     `json:"insecure,omitempty"`
	ALPN            []string `json:"alpn,omitempty"`
	PublicKeyPins   []string `json:"public_key_pins,omitempty"`
	CertificateHash string   `json:"certificate_hash,omitempty"`
	Fingerprint     string   `json:"fingerprint,omitempty"`
	RealityKey      string   `json:"reality_key,omitempty"`
	RealityShortID  string   `json:"reality_short_id,omitempty"`
}

type Transport struct {
	Type            string        `json:"type,omitempty"`
	Path            string        `json:"path,omitempty"`
	Host            string        `json:"host,omitempty"`
	ServiceName     string        `json:"service_name,omitempty"`
	Mode            string        `json:"mode,omitempty"`
	Authority       string        `json:"authority,omitempty"`
	EarlyData       int           `json:"early_data,omitempty"`
	EarlyDataHeader string        `json:"early_data_header,omitempty"`
	XHTTP           *XHTTPOptions `json:"xhttp,omitempty"`
}

// Node is the only representation used between parsing, processing and adapters.
// Gateway is trusted local metadata, never accepted from a remote subscription.
type Node struct {
	ID                 string    `json:"id"`
	SourceID           string    `json:"source_id,omitempty"`
	Name               string    `json:"name"`
	Tags               []string  `json:"tags,omitempty"`
	Gateway            bool      `json:"gateway,omitempty"`
	Protocol           string    `json:"protocol"`
	Server             string    `json:"server"`
	Port               int       `json:"port"`
	UUID               string    `json:"uuid,omitempty"`
	Password           string    `json:"password,omitempty"`
	Cipher             string    `json:"cipher,omitempty"`
	Flow               string    `json:"flow,omitempty"`
	TLS                TLS       `json:"tls,omitempty"`
	Transport          Transport `json:"transport,omitempty"`
	Ports              []string  `json:"ports,omitempty"`
	HopIntervalSeconds int       `json:"hop_interval_seconds,omitempty"`
	Congestion         string    `json:"congestion,omitempty"`
	UDPRelay           string    `json:"udp_relay,omitempty"`
	Obfs               string    `json:"obfs,omitempty"`
	ObfsPassword       string    `json:"obfs_password,omitempty"`
	Plugin             *Plugin   `json:"plugin,omitempty"`
}

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
var nodeUUIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
var protocols = []string{"vless", "vmess", "hysteria2", "tuic", "anytls", "trojan", "ss"}

func ValidID(s string) bool     { return idPattern.MatchString(s) }
func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func (n Node) Identity() string {
	n.ID, n.SourceID, n.Name, n.Tags = "", "", "", nil
	data, _ := json.Marshal(n)
	return Digest(data)
}

func (n Node) Validate() error {
	if !slices.Contains(protocols, n.Protocol) {
		return fmt.Errorf("unsupported protocol %q", n.Protocol)
	}
	if n.Port < 1 || n.Port > 65535 || domain.ValidateHost(n.Server) != nil {
		return errors.New("invalid server or port")
	}
	if n.Name == "" || len(n.Name) > 256 || strings.ContainsAny(n.Name, "\r\n\x00") {
		return errors.New("invalid node name")
	}
	if n.ID != "" && !ValidID(n.ID) {
		return errors.New("invalid node ID")
	}
	if n.Protocol == "vless" || n.Protocol == "vmess" || n.Protocol == "tuic" {
		if !nodeUUIDPattern.MatchString(n.UUID) {
			return errors.New("invalid UUID")
		}
	}
	if slices.Contains([]string{"hysteria2", "tuic", "anytls", "trojan", "ss"}, n.Protocol) && n.Password == "" {
		return errors.New("missing password")
	}
	if len(n.Password) > 4096 || strings.ContainsAny(n.Password, "\r\n\x00") {
		return errors.New("invalid password")
	}
	if !slices.Contains([]string{"", "tcp", "ws", "grpc", "httpupgrade", "xhttp"}, n.Transport.Type) {
		return errors.New("unsupported transport")
	}
	if slices.Contains([]string{"ws", "httpupgrade", "xhttp"}, n.Transport.Type) && (!strings.HasPrefix(n.Transport.Path, "/") || strings.ContainsAny(n.Transport.Path+n.Transport.Host, "\r\n\x00")) {
		return errors.New("invalid WebSocket transport")
	}
	if !slices.Contains([]string{"ws", "httpupgrade", "xhttp"}, n.Transport.Type) && (n.Transport.Path != "" || n.Transport.Host != "") {
		return errors.New("path and host require WebSocket or HTTPUpgrade")
	}
	if n.Flow != "" && (n.Protocol != "vless" || n.Flow != "xtls-rprx-vision") {
		return errors.New("unsupported flow")
	}
	if n.Flow != "" && (!n.TLS.Enabled || (n.Transport.Type != "" && n.Transport.Type != "tcp")) {
		return errors.New("Vision requires TLS over TCP")
	}
	if len(n.Tags) > 64 || len(n.TLS.ALPN) > 16 || len(n.Ports) > 256 {
		return errors.New("node list field limit exceeded")
	}
	if n.TLS.RealityKey != "" && (!n.TLS.Enabled || n.Protocol != "vless") {
		return errors.New("Reality requires VLESS TLS")
	}
	if len(n.Ports) > 0 && n.Protocol != "hysteria2" {
		return errors.New("port hopping requires Hysteria2")
	}
	if n.HopIntervalSeconds != 0 && (n.Protocol != "hysteria2" || len(n.Ports) == 0 || n.HopIntervalSeconds < 5 || n.HopIntervalSeconds > 86400) {
		return errors.New("port hopping interval requires Hysteria2 ports and 5–86400 seconds")
	}
	for _, p := range n.Ports {
		if !portRangePattern.MatchString(p) {
			return errors.New("invalid hopping ports")
		}
		parts := strings.FieldsFunc(p, func(r rune) bool { return r == ':' || r == '-' })
		first, _ := strconv.Atoi(parts[0])
		last := first
		if len(parts) == 2 {
			last, _ = strconv.Atoi(parts[1])
		}
		if first < 1 || last > 65535 || first > last {
			return errors.New("invalid hopping port range")
		}
	}
	if n.TLS.RealityKey != "" {
		key, err := base64.RawURLEncoding.DecodeString(n.TLS.RealityKey)
		if err != nil || len(key) != 32 {
			return errors.New("invalid Reality public key")
		}
		sid, err := hex.DecodeString(n.TLS.RealityShortID)
		if err != nil || len(sid) > 8 {
			return errors.New("invalid Reality short ID")
		}
	}
	for _, pin := range n.TLS.PublicKeyPins {
		value, err := base64.StdEncoding.DecodeString(pin)
		if err != nil || len(value) != 32 {
			return errors.New("invalid certificate public-key pin")
		}
	}
	if n.TLS.CertificateHash != "" {
		value, err := hex.DecodeString(n.TLS.CertificateHash)
		if err != nil || len(value) != 32 {
			return errors.New("invalid certificate hash")
		}
	}
	for _, alpn := range n.TLS.ALPN {
		if len(alpn) == 0 || len(alpn) > 255 || strings.ContainsAny(alpn, "\r\n\x00") {
			return errors.New("invalid ALPN")
		}
	}
	if (n.TLS.ServerName != "" && domain.ValidateHost(n.TLS.ServerName) != nil) || strings.ContainsAny(n.Transport.Host, "\r\n\x00") {
		return errors.New("invalid TLS/transport name")
	}
	if slices.Contains([]string{"hysteria2", "tuic", "anytls", "trojan"}, n.Protocol) && !n.TLS.Enabled {
		return errors.New("protocol requires TLS")
	}
	if n.Transport.Type != "" && n.Transport.Type != "tcp" && !slices.Contains([]string{"vmess", "vless", "trojan"}, n.Protocol) {
		return errors.New("protocol does not support this transport")
	}
	if len(n.Transport.ServiceName) > 256 || strings.ContainsAny(n.Transport.ServiceName, "\r\n\x00") || (n.Transport.ServiceName != "" && n.Transport.Type != "grpc") {
		return errors.New("invalid gRPC service name")
	}
	if n.Obfs != "" && (n.Protocol != "hysteria2" || n.Obfs != "salamander" || n.ObfsPassword == "") {
		return errors.New("unsupported obfuscation")
	}
	if len(n.ObfsPassword) > 4096 || strings.ContainsAny(n.ObfsPassword, "\r\n\x00") || (n.ObfsPassword != "" && n.Obfs == "") {
		return errors.New("invalid obfuscation password")
	}
	if n.Protocol == "vmess" && !slices.Contains([]string{"", "auto", "none", "zero", "aes-128-gcm", "chacha20-poly1305"}, n.Cipher) {
		return errors.New("unsupported VMess security")
	}
	if n.Congestion != "" && (n.Protocol != "tuic" || !slices.Contains([]string{"bbr", "cubic", "new_reno"}, n.Congestion)) {
		return errors.New("unsupported congestion controller")
	}
	if n.UDPRelay != "" && (n.Protocol != "tuic" || !slices.Contains([]string{"native", "quic"}, n.UDPRelay)) {
		return errors.New("unsupported UDP relay mode")
	}
	if n.Protocol == "ss" && !slices.Contains([]string{"aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305"}, n.Cipher) {
		return errors.New("unsupported Shadowsocks cipher")
	}
	return validateConnectionOptions(n)
}

var portRangePattern = regexp.MustCompile(`^[0-9]{1,5}([:-][0-9]{1,5})?$`)

type Processor struct {
	Protocols         []string `json:"protocols,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	Include           string   `json:"include,omitempty"`
	Exclude           string   `json:"exclude,omitempty"`
	RenamePattern     string   `json:"rename_pattern,omitempty"`
	RenameReplacement string   `json:"rename_replacement,omitempty"`
	Prefix            string   `json:"prefix,omitempty"`
	Deduplicate       bool     `json:"deduplicate,omitempty"`
	Sort              bool     `json:"sort,omitempty"`
}

type Diagnostic struct {
	NodeID  string `json:"node_id,omitempty"`
	Name    string `json:"name,omitempty"`
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type Preview struct {
	Input       int          `json:"input"`
	Output      int          `json:"output"`
	Nodes       []Node       `json:"nodes"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func Process(nodes []Node, p Processor) (Preview, error) {
	result := Preview{Input: len(nodes), Nodes: []Node{}, Diagnostics: []Diagnostic{}}
	if len(nodes) > MaxNodes {
		return result, errors.New("node limit exceeded")
	}
	for _, protocol := range p.Protocols {
		if !slices.Contains(protocols, protocol) {
			return result, errors.New("unknown processor protocol")
		}
	}
	if len(p.Prefix) > 128 || len(p.Tags) > 64 {
		return result, errors.New("processor size limit exceeded")
	}
	patterns := make([]*regexp.Regexp, 3)
	for i, s := range []string{p.Include, p.Exclude, p.RenamePattern} {
		if len(s) > 1024 {
			return result, errors.New("processor expression too long")
		}
		if s != "" {
			r, err := regexp.Compile(s)
			if err != nil {
				return result, err
			}
			patterns[i] = r
		}
	}
	seen, names := map[string]bool{}, newNodeNames()
	for _, original := range nodes {
		n := normalizeNode(original)
		if err := n.Validate(); err != nil {
			return result, fmt.Errorf("node %s: %w", n.ID, err)
		}
		reason := ""
		if len(p.Protocols) > 0 && !slices.Contains(p.Protocols, n.Protocol) {
			reason = "protocol filter"
		}
		if len(p.Tags) > 0 && !slices.ContainsFunc(p.Tags, func(t string) bool { return slices.Contains(n.Tags, t) }) {
			reason = "tag filter"
		}
		if patterns[0] != nil && !patterns[0].MatchString(n.Name) {
			reason = "include filter"
		}
		if patterns[1] != nil && patterns[1].MatchString(n.Name) {
			reason = "exclude filter"
		}
		identity := ""
		if p.Deduplicate {
			identity = n.Identity()
			if seen[identity] {
				reason = "duplicate connection parameters"
			}
		}
		if reason != "" {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{n.ID, n.Name, "filter", reason})
			continue
		}
		if p.Deduplicate {
			seen[identity] = true
		}
		if patterns[2] != nil {
			n.Name = patterns[2].ReplaceAllString(n.Name, p.RenameReplacement)
		}
		n.Name = p.Prefix + n.Name
		n.Name = names.unique(n.Name)
		if n.Name != original.Name {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{n.ID, original.Name, "rename", n.Name})
		}
		if err := n.Validate(); err != nil {
			return result, err
		}
		result.Nodes = append(result.Nodes, n)
	}
	if p.Sort {
		slices.SortStableFunc(result.Nodes, func(a, b Node) int { return strings.Compare(a.Name, b.Name) })
	}
	result.Output = len(result.Nodes)
	return result, nil
}
