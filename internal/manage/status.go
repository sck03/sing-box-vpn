package manage

import (
	"strings"
	"unicode"
)

// TerminalText removes control characters from values displayed in terminal summaries.
func TerminalText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, value)
}
