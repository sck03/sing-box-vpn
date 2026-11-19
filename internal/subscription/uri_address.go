package subscription

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var hoppingAddress = regexp.MustCompile(`^(hysteria2|hy2)://([^@]*)@(\[[^\]]+\]|[^:/?#]+):([0-9,;:-]+)([/#?].*)?$`)

func normalizeURIAddress(line string) (string, []string, error) {
	scheme, rest, found := strings.Cut(strings.TrimSpace(line), "://")
	if !found {
		return line, nil, nil
	}
	scheme = strings.ToLower(scheme)
	line = scheme + "://" + rest
	if scheme == "ss" {
		address := strings.SplitN(strings.SplitN(rest, "#", 2)[0], "?", 2)[0]
		if !strings.Contains(address, "@") {
			decoded, err := decodeBase64(address)
			if err == nil && strings.Contains(string(decoded), "@") {
				line = "ss://" + string(decoded) + rest[len(address):]
			}
		}
	}
	match := hoppingAddress.FindStringSubmatch(line)
	if len(match) == 0 || !strings.ContainsAny(match[4], ",;:-") {
		return line, nil, nil
	}
	ports := strings.FieldsFunc(match[4], func(r rune) bool { return r == ',' || r == ';' })
	if len(ports) == 0 || len(ports) > 256 {
		return line, nil, errors.New("invalid hopping address")
	}
	first := strings.FieldsFunc(ports[0], func(r rune) bool { return r == '-' || r == ':' })
	if len(first) == 0 {
		return line, nil, errors.New("invalid hopping address")
	}
	port, err := strconv.Atoi(first[0])
	if err != nil || port < 1 || port > 65535 {
		return line, nil, errors.New("invalid hopping address")
	}
	return match[1] + "://" + match[2] + "@" + match[3] + ":" + first[0] + match[5], ports, nil
}

func normalizeWebSocketPath(t Transport) Transport {
	if t.Type != "ws" || t.EarlyData != 0 {
		return t
	}
	path, query, found := strings.Cut(t.Path, "?")
	if !found {
		return t
	}
	values, err := url.ParseQuery(query)
	if err != nil || len(values["ed"]) != 1 {
		return t
	}
	n, err := strconv.Atoi(values.Get("ed"))
	if err != nil || n < 1 || n > 65535 {
		return t
	}
	parts := []string{}
	for _, part := range strings.Split(query, "&") {
		key, _, _ := strings.Cut(part, "=")
		decoded, _ := url.QueryUnescape(key)
		if decoded != "ed" {
			parts = append(parts, part)
		}
	}
	if len(parts) > 0 {
		path += "?" + strings.Join(parts, "&")
	}
	t.Path, t.EarlyData, t.EarlyDataHeader = path, n, "Sec-WebSocket-Protocol"
	return t
}
