package subscription

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Plugin contains portable SIP003 options, never a command or executable path.
type Plugin struct {
	Type string `json:"type"`
	Mode string `json:"mode"`
	Host string `json:"host,omitempty"`
	Path string `json:"path,omitempty"`
	TLS  bool   `json:"tls,omitempty"`
	Mux  bool   `json:"mux,omitempty"`
}

// XHTTP keeps URI extra keys in their documented spelling. Each renderer maps
// the supported subset explicitly; arbitrary dialers and downloadSettings are
// rejected rather than being silently dropped or bypassing the node model.
type XHTTPOptions struct {
	Headers       map[string]string `json:"headers,omitempty"`
	NoSSEHeader   *bool             `json:"noSSEHeader,omitempty"`
	NoGRPCHeader  *bool             `json:"noGRPCHeader,omitempty"`
	Padding       string            `json:"xPaddingBytes,omitempty"`
	PostBytes     string            `json:"scMaxEachPostBytes,omitempty"`
	PostInterval  string            `json:"scMinPostsIntervalMs,omitempty"`
	BufferedPosts int               `json:"scMaxBufferedPosts,omitempty"`
	StreamSeconds string            `json:"scStreamUpServerSecs,omitempty"`
}

var transportRange = regexp.MustCompile(`^[0-9]{1,9}(-[0-9]{1,9})?$`)
var headerNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

func validateConnectionOptions(n Node) error {
	t := n.Transport
	if len(t.Path) > 8192 || len(t.Host) > 253 || len(t.Authority) > 253 || strings.ContainsAny(t.Authority, "\r\n\x00") {
		return errors.New("transport field exceeds limits")
	}
	if t.Type == "grpc" {
		if !slices.Contains([]string{"", "gun", "multi"}, t.Mode) {
			return errors.New("unsupported gRPC mode")
		}
	} else if t.Type == "xhttp" {
		if n.Protocol != "vless" || !slices.Contains([]string{"", "auto", "packet-up", "stream-up", "stream-one"}, t.Mode) {
			return errors.New("unsupported XHTTP protocol or mode")
		}
	} else if t.Mode != "" {
		return errors.New("transport mode requires gRPC or XHTTP")
	}
	if t.Authority != "" && t.Type != "grpc" {
		return errors.New("authority requires gRPC")
	}
	if t.EarlyData < 0 || t.EarlyData > 65535 || len(t.EarlyDataHeader) > 128 || strings.ContainsAny(t.EarlyDataHeader, " :\r\n\x00") {
		return errors.New("invalid WebSocket early data")
	}
	if (t.EarlyData != 0 || t.EarlyDataHeader != "") && t.Type != "ws" {
		return errors.New("early data requires WebSocket")
	}
	if t.XHTTP != nil {
		if t.Type != "xhttp" {
			return errors.New("XHTTP options require XHTTP")
		}
		x := t.XHTTP
		if len(x.Headers) > 16 || x.BufferedPosts < 0 || x.BufferedPosts > 65535 {
			return errors.New("XHTTP options exceed limits")
		}
		for key, value := range x.Headers {
			if key == "" || http.CanonicalHeaderKey(key) == "" || strings.ContainsAny(key, " \t\r\n:\x00") || strings.ContainsAny(value, "\r\n\x00") || len(value) > 4096 {
				return errors.New("invalid XHTTP header")
			}
		}
		for _, value := range []string{x.Padding, x.PostBytes, x.PostInterval, x.StreamSeconds} {
			if value == "" {
				continue
			}
			if !transportRange.MatchString(value) {
				return errors.New("invalid XHTTP range")
			}
			parts := strings.Split(value, "-")
			if len(parts) == 2 {
				a, _ := strconv.Atoi(parts[0])
				b, _ := strconv.Atoi(parts[1])
				if a > b {
					return errors.New("reversed XHTTP range")
				}
			}
		}
	}
	if n.Plugin != nil {
		p := n.Plugin
		if n.Protocol != "ss" || n.TLS.Enabled || (t.Type != "" && t.Type != "tcp") {
			return errors.New("SIP003 plugin requires plain Shadowsocks transport")
		}
		if !slices.Contains([]string{"obfs", "v2ray-plugin"}, p.Type) || p.Type == "obfs" && !slices.Contains([]string{"http", "tls"}, p.Mode) || p.Type == "v2ray-plugin" && p.Mode != "websocket" {
			return errors.New("unsupported SIP003 plugin or mode")
		}
		if p.Type == "obfs" && (p.TLS || p.Mux || p.Path != "") {
			return errors.New("obfs plugin cannot preserve TLS, mux or path options")
		}
		if len(p.Host) > 253 || len(p.Path) > 8192 || strings.ContainsAny(p.Host+p.Path, ";\r\n\x00") || p.Path != "" && !strings.HasPrefix(p.Path, "/") {
			return errors.New("invalid plugin host or path")
		}
	}
	return nil
}

func parsePlugin(value string) (*Plugin, error) {
	fields := strings.Split(value, ";")
	p := &Plugin{Type: fields[0]}
	if p.Type == "simple-obfs" || p.Type == "obfs-local" {
		p.Type = "obfs"
	}
	if p.Type == "v2ray-plugin" {
		p.Mode = "websocket"
	}
	seen := map[string]bool{}
	for _, field := range fields[1:] {
		if field == "" {
			continue
		}
		key, v, hasValue := strings.Cut(field, "=")
		if key == "obfs" {
			key = "mode"
		}
		if key == "obfs-host" {
			key = "host"
		}
		if seen[key] {
			return nil, errors.New("duplicate plugin option")
		}
		seen[key] = true
		switch key {
		case "mode":
			p.Mode = v
		case "host":
			p.Host = v
		case "path":
			p.Path = v
		case "tls", "mux":
			enabled := true
			if hasValue {
				var err error
				enabled, err = strconv.ParseBool(v)
				if err != nil {
					return nil, errors.New("invalid plugin flag")
				}
			}
			if key == "tls" {
				p.TLS = enabled
			} else {
				p.Mux = enabled
			}
		default:
			return nil, fmt.Errorf("unsupported plugin option %s", key)
		}
	}
	return p, nil
}

func (p Plugin) options() string {
	parts := []string{"mode=" + p.Mode}
	if p.Type == "obfs" {
		parts[0] = "obfs=" + p.Mode
	}
	if p.Host != "" {
		key := "host="
		if p.Type == "obfs" {
			key = "obfs-host="
		}
		parts = append(parts, key+p.Host)
	}
	if p.Path != "" {
		parts = append(parts, "path="+p.Path)
	}
	if p.TLS {
		parts = append(parts, "tls")
	}
	if p.Mux {
		parts = append(parts, "mux")
	}
	return strings.Join(parts, ";")
}

func decodeXHTTP(data []byte) (*XHTTPOptions, error) {
	// Accept JSON numbers for range-valued fields, but preserve all semantics.
	var value Object
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	for _, key := range []string{"xPaddingBytes", "scMaxEachPostBytes", "scMinPostsIntervalMs", "scStreamUpServerSecs"} {
		if n, ok := value[key].(float64); ok {
			if n < 0 || n != float64(int(n)) || n > 999999999 {
				return nil, errors.New("invalid XHTTP number")
			}
			value[key] = strconv.Itoa(int(n))
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var x XHTTPOptions
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&x); err != nil {
		return nil, errors.New("unsupported or invalid XHTTP extra options")
	}
	return &x, nil
}

var xhttpNames = map[string]string{
	"headers": "headers", "no-sse-header": "noSSEHeader", "no-grpc-header": "noGRPCHeader",
	"x-padding-bytes": "xPaddingBytes", "sc-max-each-post-bytes": "scMaxEachPostBytes",
	"sc-min-posts-interval-ms": "scMinPostsIntervalMs", "sc-max-buffered-posts": "scMaxBufferedPosts", "sc-stream-up-server-secs": "scStreamUpServerSecs",
}

func xhttpFromNative(native Object) (*XHTTPOptions, error) {
	extra := Object{}
	for key, value := range native {
		if slices.Contains([]string{"path", "host", "mode"}, key) {
			if _, ok := value.(string); !ok {
				return nil, errors.New("invalid XHTTP transport field")
			}
			continue
		}
		name, ok := xhttpNames[key]
		if !ok {
			return nil, fmt.Errorf("unsupported XHTTP option %s", key)
		}
		extra[name] = value
	}
	if len(extra) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(extra)
	if err != nil {
		return nil, err
	}
	return decodeXHTTP(data)
}

func (x *XHTTPOptions) native() Object {
	result := Object{}
	if x == nil {
		return result
	}
	if len(x.Headers) > 0 {
		result["headers"] = x.Headers
	}
	if x.NoSSEHeader != nil {
		result["no-sse-header"] = *x.NoSSEHeader
	}
	if x.NoGRPCHeader != nil {
		result["no-grpc-header"] = *x.NoGRPCHeader
	}
	if x.BufferedPosts != 0 {
		result["sc-max-buffered-posts"] = x.BufferedPosts
	}
	for key, value := range map[string]string{"x-padding-bytes": x.Padding, "sc-max-each-post-bytes": x.PostBytes, "sc-min-posts-interval-ms": x.PostInterval, "sc-stream-up-server-secs": x.StreamSeconds} {
		if value != "" {
			result[key] = value
		}
	}
	return result
}
