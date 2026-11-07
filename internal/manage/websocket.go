package manage

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The server compares URL.Path with the configured path, while clients parse
// the setting as a URL. Query strings and escapes would change that comparison.
func validateWebSocketPath(path string) error {
	if len(path) == 0 || len(path) > 255 || path[0] != '/' || !utf8.ValidString(path) ||
		strings.ContainsAny(path, "?#%\\") || strings.ContainsFunc(path, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) {
		return errors.New("WebSocket 路径须以 / 开头，最多 255 字节，不能包含查询参数、片段、百分号转义、反斜杠、空白或控制字符")
	}
	return nil
}

func webSocketHost(transport Object, fallback string) string {
	if host := text(object(transport["headers"])["Host"]); host != "" {
		return host
	}
	return fallback
}
