package subscription

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

type ContentPreview struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	Decoded   bool   `json:"decoded"`
}

var previewURL = regexp.MustCompile(`https?://[^\s,"']+`)

func PreviewArtifact(artifact Artifact, client string, limit int) (ContentPreview, error) {
	result := ContentPreview{}
	if limit < 1 || limit > MaxBody {
		return result, errors.New("invalid preview limit")
	}
	var text string
	if client == "shadowrocket" && artifact.Extension == "conf" {
		text = previewURL.ReplaceAllString(string(artifact.Data), "REDACTED-URL")
	} else if artifact.MIME == "application/json" || artifact.MIME == "application/yaml" {
		var value any
		if err := json.Unmarshal(artifact.Data, &value); err != nil {
			return result, err
		}
		redactPreview(value)
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return result, err
		}
		text = string(data)
	} else {
		data := artifact.Data
		format := client
		if client == "base64" || client == "shadowrocket" {
			var err error
			data, err = decodeBase64(strings.TrimSpace(string(data)))
			if err != nil {
				return result, err
			}
			format = "uri"
			result.Decoded = true
		}
		if client == "surge-mac" || client == "surfboard" {
			format = "surge"
		}
		nodes, err := Parse(data, format, "preview")
		if err != nil {
			return result, errors.New("output cannot be safely previewed")
		}
		if format == "uri" {
			lines := make([]string, 0, len(nodes))
			for _, n := range nodes {
				if n.UUID != "" {
					n.UUID = "00000000-0000-4000-8000-000000000000"
				}
				if n.Password != "" {
					n.Password = "REDACTED"
				}
				if n.ObfsPassword != "" {
					n.ObfsPassword = "REDACTED"
				}
				lines = append(lines, n.URI())
			}
			text = strings.Join(lines, "\n")
		} else {
			text = string(data)
			for _, n := range nodes {
				for _, secret := range []string{n.UUID, n.Password, n.ObfsPassword} {
					if secret != "" {
						text = strings.ReplaceAll(text, secret, "REDACTED")
					}
				}
			}
			text = previewURL.ReplaceAllString(text, "REDACTED-URL")
		}
	}
	if len(text) > limit {
		text = text[:limit]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		result.Truncated = true
	}
	result.Content = text
	return result, nil
}

func redactPreview(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			name := strings.ReplaceAll(strings.ToLower(key), "-", "_")
			if slices.Contains([]string{"uuid", "user_id", "username", "password", "passwords", "auth", "auth_str", "token", "token_hash", "secret", "private_key", "pre_shared_key", "url", "webhook", "headers", "authorization", "client_secret", "proxy_pass", "socks_pass", "obfs_password", "key", "api_key"}, name) {
				v[key] = "REDACTED"
			} else {
				redactPreview(item)
			}
		}
	case []any:
		for _, item := range v {
			redactPreview(item)
		}
	}
}
