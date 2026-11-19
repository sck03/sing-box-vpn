package subscription

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

func decodeBase64(s string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if data, err := encoding.DecodeString(s); err == nil {
			return data, nil
		}
	}
	return nil, errors.New("invalid base64")
}

type ParseResult struct {
	Format      string       `json:"format"`
	Nodes       []Node       `json:"nodes"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func InputFormats() []string {
	return []string{"auto", "uri", "base64", "mihomo", "sing-box", "nodes", "surge", "loon", "quantumult-x"}
}

// Parsers operate on bounded data, never execute scripts or resolve local files.
func Parse(data []byte, format, sourceID string) ([]Node, error) {
	result, err := ParseSource(data, Source{ID: sourceID, Format: format})
	return result.Nodes, err
}

func ParseSource(data []byte, source Source) (ParseResult, error) {
	result := ParseResult{Format: source.Format, Nodes: []Node{}, Diagnostics: []Diagnostic{}}
	if len(data) == 0 || len(data) > MaxBody {
		return result, errors.New("empty or oversized source")
	}
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	format := source.Format
	if format == "auto" {
		format = detectFormat(data)
		if format == "" {
			return result, errors.New("cannot identify subscription format")
		}
	}
	if format == "base64" {
		decoded, err := decodeBase64(strings.Join(strings.Fields(string(data)), ""))
		if err != nil {
			return result, err
		}
		data, format = decoded, "uri"
	}
	result.Format = format
	var nodes []Node
	count := 0
	accept := func(n Node, err error) error {
		count++
		if count > MaxNodes {
			return errors.New("node limit exceeded")
		}
		n = normalizeNode(n)
		if err == nil {
			err = n.Validate()
		}
		if err != nil {
			if !source.SkipInvalid {
				return fmt.Errorf("node %d: %w", count, err)
			}
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Name: n.Name, Stage: "parse", Message: fmt.Sprintf("node %d: %s", count, err)})
			return nil
		}
		nodes = append(nodes, n)
		return nil
	}
	switch format {
	case "uri":
		for line := range strings.SplitSeq(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			n, err := ParseURI(line)
			if err = accept(n, err); err != nil {
				return result, err
			}
		}
	case "nodes":
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&nodes); err != nil {
			return result, err
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return result, errors.New("expected one JSON document")
		}
		input := nodes
		nodes = nil
		for _, n := range input {
			if err := accept(n, nil); err != nil {
				return result, err
			}
		}
	case "mihomo", "sing-box":
		var document struct {
			Proxies   []map[string]any `yaml:"proxies"`
			Outbounds []map[string]any `yaml:"outbounds"`
		}
		var tree yaml.Node
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		if err := decoder.Decode(&tree); err != nil {
			return result, err
		}
		if err := decoder.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
			return result, errors.New("expected one YAML document")
		}
		if err := validateYAMLTree(&tree, 0); err != nil {
			return result, err
		}
		if err := tree.Decode(&document); err != nil {
			return result, err
		}
		items := document.Proxies
		if format == "sing-box" {
			items = document.Outbounds
		}
		for i, item := range items {
			if i >= MaxNodes {
				return result, errors.New("node limit exceeded")
			}
			var n Node
			var err error
			if format == "sing-box" {
				if kind := str(item["type"]); kind == "selector" || kind == "urltest" || kind == "direct" || kind == "block" || kind == "dns" {
					continue
				}
				n, err = FromSingBox(item)
			} else {
				n, err = fromMihomo(item)
			}
			if err = accept(n, err); err != nil {
				return result, err
			}
		}
	case "surge", "loon", "quantumult-x":
		active := true
		for line := range strings.SplitSeq(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[") {
				active = strings.EqualFold(line, "[Proxy]") || strings.EqualFold(line, "[server_local]")
				continue
			}
			if !active || line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "//") {
				continue
			}
			n, err := parseTextNode(line, format)
			if err = accept(n, err); err != nil {
				return result, err
			}
		}
	default:
		return result, fmt.Errorf("unknown input format %q", format)
	}
	if len(nodes) == 0 || len(nodes) > MaxNodes {
		return result, errors.New("source must contain 1-4096 valid nodes")
	}
	seen := map[string]bool{}
	for i := range nodes {
		n := &nodes[i]
		n.Gateway = false
		n.SourceID = source.ID
		n.ID = Digest([]byte(source.ID + ":" + n.Identity()))[:24]
		// Identical nodes still pass through Process's explicit deduplication rule.
		if seen[n.ID] {
			n.ID = Digest([]byte(n.ID + ":" + strconv.Itoa(i)))[:24]
		}
		seen[n.ID] = true
	}
	result.Nodes = nodes
	return result, nil
}

func validateYAMLTree(n *yaml.Node, depth int) error {
	if depth > 32 || n.Kind == yaml.AliasNode {
		return errors.New("YAML aliases or excessive nesting are not supported")
	}
	for _, child := range n.Content {
		if err := validateYAMLTree(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func str(v any) string         { s, _ := v.(string); return s }
func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func integer(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		if !math.IsNaN(x) && !math.IsInf(x, 0) && x >= 0 && x <= math.MaxInt32 && x == math.Trunc(x) {
			return int(x)
		}
		return -1
	case string:
		n, err := strconv.Atoi(x)
		if err != nil {
			return -1
		}
		return n
	}
	return 0
}
func boolean(v any) bool { b, _ := v.(bool); return b }
func stringList(v any) []string {
	if s, ok := v.([]string); ok {
		return s
	}
	var out []string
	if values, ok := v.([]any); ok {
		for _, value := range values {
			out = append(out, str(value))
		}
	}
	return out
}

func FromSingBox(m Object) (Node, error) {
	if err := singBoxFields(m); err != nil {
		return Node{}, err
	}
	n := Node{Name: str(m["tag"]), Protocol: str(m["type"]), Server: str(m["server"]), Port: integer(m["server_port"]), UUID: str(m["uuid"]), Password: str(m["password"]), Flow: str(m["flow"]), Cipher: str(m["security"]), Congestion: str(m["congestion_control"]), UDPRelay: str(m["udp_relay_mode"]), Ports: stringList(m["server_ports"])}
	if n.Protocol == "shadowsocks" {
		n.Protocol, n.Cipher = "ss", str(m["method"])
	}
	if value, exists := m["hop_interval"]; exists {
		var err error
		n.HopIntervalSeconds, err = hoppingInterval(str(value))
		if err != nil {
			return n, err
		}
	}
	t := obj(m["tls"])
	reality := obj(t["reality"])
	n.TLS = TLS{Enabled: boolean(t["enabled"]), ServerName: str(t["server_name"]), Insecure: boolean(t["insecure"]), ALPN: stringList(t["alpn"]), PublicKeyPins: stringList(t["certificate_public_key_sha256"]), Fingerprint: str(obj(t["utls"])["fingerprint"]), RealityKey: str(reality["public_key"]), RealityShortID: str(reality["short_id"])}
	transport := obj(m["transport"])
	n.Transport = Transport{Type: str(transport["type"]), Path: str(transport["path"]), Host: str(obj(transport["headers"])["Host"])}
	n.Transport.EarlyData, n.Transport.EarlyDataHeader = integer(transport["max_early_data"]), str(transport["early_data_header_name"])
	if n.Transport.Type == "grpc" {
		n.Transport.ServiceName = str(transport["service_name"])
	}
	if n.Transport.Type == "httpupgrade" {
		n.Transport.Host = str(transport["host"])
	}
	n.Obfs, n.ObfsPassword = str(obj(m["obfs"])["type"]), str(obj(m["obfs"])["password"])
	if plugin := str(m["plugin"]); plugin != "" {
		var err error
		n.Plugin, err = parsePlugin(plugin + ";" + str(m["plugin_opts"]))
		if err != nil {
			return n, err
		}
	} else if str(m["plugin_opts"]) != "" {
		return n, errors.New("plugin options require a plugin")
	}
	n = normalizeNode(n)
	return n, n.Validate()
}

func fromMihomo(m Object) (Node, error) {
	if err := mihomoFields(m); err != nil {
		return Node{}, err
	}
	n := Node{Name: str(m["name"]), Protocol: str(m["type"]), Server: str(m["server"]), Port: integer(m["port"]), UUID: str(m["uuid"]), Password: str(m["password"]), Cipher: str(m["cipher"]), Flow: str(m["flow"]), Congestion: str(m["congestion-controller"]), UDPRelay: str(m["udp-relay-mode"])}
	if integer(m["alterId"]) != 0 {
		return n, errors.New("legacy VMess alterId is unsupported")
	}
	r := obj(m["reality-opts"])
	n.TLS = TLS{Enabled: boolean(m["tls"]) || n.Protocol == "hysteria2" || n.Protocol == "tuic" || n.Protocol == "anytls" || n.Protocol == "trojan", ServerName: fallback(str(m["servername"]), str(m["sni"])), Insecure: boolean(m["skip-cert-verify"]), ALPN: stringList(m["alpn"]), CertificateHash: str(m["fingerprint"]), Fingerprint: str(m["client-fingerprint"]), RealityKey: str(r["public-key"]), RealityShortID: str(r["short-id"])}
	w := obj(m["ws-opts"])
	n.Transport = Transport{Type: str(m["network"]), Path: str(w["path"]), Host: str(obj(w["headers"])["Host"])}
	n.Transport.EarlyData, n.Transport.EarlyDataHeader = integer(w["max-early-data"]), str(w["early-data-header-name"])
	if boolean(w["v2ray-http-upgrade"]) {
		n.Transport.Type = "httpupgrade"
	}
	if n.Transport.Type == "grpc" {
		n.Transport.ServiceName = str(obj(m["grpc-opts"])["grpc-service-name"])
	}
	if n.Transport.Type == "xhttp" {
		x := obj(m["xhttp-opts"])
		n.Transport.Path, n.Transport.Host, n.Transport.Mode = str(x["path"]), str(x["host"]), str(x["mode"])
		var err error
		n.Transport.XHTTP, err = xhttpFromNative(x)
		if err != nil {
			return n, err
		}
	}
	if plugin := str(m["plugin"]); plugin != "" {
		p := obj(m["plugin-opts"])
		n.Plugin = &Plugin{Type: plugin, Mode: str(p["mode"]), Host: str(p["host"]), Path: str(p["path"]), TLS: boolean(p["tls"]), Mux: boolean(p["mux"])}
	} else if len(obj(m["plugin-opts"])) > 0 {
		return n, errors.New("plugin options require a plugin")
	}
	n.Obfs, n.ObfsPassword = str(m["obfs"]), str(m["obfs-password"])
	if ports := str(m["ports"]); ports != "" {
		n.Ports = strings.Split(ports, ",")
	}
	n.HopIntervalSeconds = integer(m["hop-interval"])
	n = normalizeNode(n)
	return n, n.Validate()
}

func ParseURI(line string) (Node, error) {
	line, hopping, err := normalizeURIAddress(line)
	if err != nil {
		return Node{}, err
	}
	if strings.HasPrefix(line, "vmess://") {
		data, err := decodeBase64(strings.TrimPrefix(line, "vmess://"))
		if err != nil {
			return Node{}, err
		}
		var m Object
		if err := json.Unmarshal(data, &m); err != nil {
			return Node{}, err
		}
		if err := nativeFields(m, "v ps add id scy net type host path tls sni fp alpn eh", "port aid ed", "", "", ""); err != nil {
			// The widely used VMess insecure flag is a string in exported JSON.
			copy := Object{}
			for key, value := range m {
				if key != "insecure" {
					copy[key] = value
				}
			}
			if err := nativeFields(copy, "v ps add id scy net type host path tls sni fp alpn eh", "port aid ed", "", "", ""); err != nil {
				return Node{}, err
			}
			if value := m["insecure"]; value != "1" && value != "0" && value != true && value != false {
				return Node{}, errors.New("invalid VMess TLS verification flag")
			}
		}
		if value := str(m["tls"]); value != "" && value != "tls" {
			return Node{}, errors.New("unsupported VMess TLS")
		}
		if value := str(m["type"]); value != "" && value != "none" {
			return Node{}, errors.New("unsupported VMess transport disguise")
		}
		if integer(m["aid"]) != 0 {
			return Node{}, errors.New("legacy VMess alterId is unsupported")
		}
		n := Node{Name: fallback(str(m["ps"]), str(m["add"])), Protocol: "vmess", Server: str(m["add"]), Port: integer(m["port"]), UUID: str(m["id"]), Cipher: fallback(str(m["scy"]), "auto"), Transport: Transport{Type: str(m["net"]), Host: str(m["host"]), Path: str(m["path"])}, TLS: TLS{Enabled: str(m["tls"]) == "tls", ServerName: str(m["sni"]), Insecure: str(m["insecure"]) == "1" || boolean(m["insecure"])}}
		n.TLS.Fingerprint = str(m["fp"])
		n.Transport.EarlyData, n.Transport.EarlyDataHeader = integer(m["ed"]), str(m["eh"])
		if n.Transport.Type == "grpc" {
			n.Transport.ServiceName, n.Transport.Path = n.Transport.Path, ""
		}
		if alpn := str(m["alpn"]); alpn != "" {
			n.TLS.ALPN = strings.Split(alpn, ",")
		}
		n = normalizeNode(n)
		return n, n.Validate()
	}
	u, err := url.Parse(line)
	if err != nil {
		return Node{}, errors.New("invalid URI")
	}
	if u.User == nil {
		return Node{}, errors.New("missing URI credentials")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Node{}, errors.New("invalid URI query")
	}
	if u.Path != "" && u.Path != "/" {
		return Node{}, errors.New("unexpected URI path")
	}
	kind := u.Scheme
	if kind == "hy2" {
		kind = "hysteria2"
	}
	n := Node{Name: fallback(u.Fragment, u.Hostname()), Protocol: kind, Server: u.Hostname(), Port: integer(u.Port()), Flow: q.Get("flow"), Congestion: q.Get("congestion_control"), UDPRelay: q.Get("udp_relay_mode"), Transport: Transport{Type: q.Get("type"), Path: q.Get("path"), Host: q.Get("host")}}
	if kind == "hysteria2" && u.Port() == "" {
		n.Port = 443
	}
	for key := range q {
		if len(q[key]) != 1 {
			return n, errors.New("duplicate URI parameter")
		}
		if !strings.Contains("|sni|peer|flow|security|encryption|fp|pbk|sid|type|path|host|serviceName|service_name|alpn|insecure|allow_insecure|allowInsecure|pinSHA256|mport|congestion_control|udp_relay_mode|obfs|obfs-password|mode|authority|ed|eh|extra|plugin|headerType|", "|"+key+"|") {
			return n, fmt.Errorf("unsupported URI parameter %s", key)
		}
	}
	for _, key := range []string{"insecure", "allow_insecure", "allowInsecure"} {
		if value := q.Get(key); value != "" && value != "0" && value != "1" && value != "true" && value != "false" {
			return n, errors.New("invalid TLS verification flag")
		}
		if q.Get(key) == "true" {
			q.Set(key, "1")
		}
	}
	if value := q.Get("security"); value != "" && value != "none" && value != "tls" && value != "reality" {
		return n, errors.New("unsupported URI security")
	}
	if q.Get("security") == "reality" && q.Get("pbk") == "" {
		return n, errors.New("Reality URI is missing public key")
	}
	if value := q.Get("encryption"); value != "" && value != "none" {
		return n, errors.New("unsupported VLESS encryption")
	}
	n.TLS = TLS{Enabled: kind != "ss" && (kind != "vless" || q.Get("security") != "none"), ServerName: fallback(q.Get("sni"), q.Get("peer")), Insecure: q.Get("insecure") == "1" || q.Get("allow_insecure") == "1" || q.Get("allowInsecure") == "1", Fingerprint: q.Get("fp"), RealityKey: q.Get("pbk"), RealityShortID: q.Get("sid"), CertificateHash: q.Get("pinSHA256")}
	if kind == "vless" && q.Get("security") == "" {
		n.TLS.Enabled = false
	}
	if alpn := q.Get("alpn"); alpn != "" {
		n.TLS.ALPN = strings.Split(alpn, ",")
	}
	if ports := q.Get("mport"); ports != "" {
		n.Ports = strings.Split(ports, ",")
	}
	if len(hopping) > 0 {
		if len(n.Ports) > 0 {
			return n, errors.New("hopping ports supplied in both address and query")
		}
		n.Ports = hopping
	}
	n.Transport.ServiceName = fallback(q.Get("serviceName"), q.Get("service_name"))
	n.Transport.Mode, n.Transport.Authority = q.Get("mode"), q.Get("authority")
	if q.Has("ed") {
		n.Transport.EarlyData = integer(q.Get("ed"))
		n.Transport.EarlyDataHeader = fallback(q.Get("eh"), "Sec-WebSocket-Protocol")
	} else if q.Has("eh") {
		return n, errors.New("early-data header requires an early-data limit")
	}
	if extra := q.Get("extra"); extra != "" {
		n.Transport.XHTTP, err = decodeXHTTP([]byte(extra))
		if err != nil {
			return n, err
		}
	}
	if plugin := q.Get("plugin"); plugin != "" {
		n.Plugin, err = parsePlugin(plugin)
		if err != nil {
			return n, err
		}
	}
	if header := q.Get("headerType"); header != "" && header != "none" {
		return n, errors.New("unsupported transport disguise")
	}
	n.Obfs, n.ObfsPassword = q.Get("obfs"), q.Get("obfs-password")
	switch kind {
	case "vless":
		if _, present := u.User.Password(); present {
			return n, errors.New("VLESS URI credentials must contain only a UUID")
		}
		n.UUID = u.User.Username()
	case "tuic":
		n.UUID = u.User.Username()
		n.Password, _ = u.User.Password()
	case "ss":
		if password, plain := u.User.Password(); plain {
			n.Cipher, n.Password = u.User.Username(), password
		} else {
			credential, err := decodeBase64(u.User.Username())
			if err != nil {
				return n, err
			}
			var ok bool
			n.Cipher, n.Password, ok = strings.Cut(string(credential), ":")
			if !ok {
				return n, errors.New("invalid Shadowsocks credential")
			}
		}
	default:
		n.Password = u.User.Username()
		if password, present := u.User.Password(); present {
			n.Password += ":" + password
		}
	}
	n = normalizeNode(n)
	return n, n.Validate()
}
