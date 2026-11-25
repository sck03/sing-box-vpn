package domain

import (
	"strings"
	"testing"
)

func TestPublicationHostBoundary(t *testing.T) {
	for _, value := range []string{"cdn.example.test", "localhost", "192.0.2.1", "2001:db8::1", "::1", "xn--fiqs8s.example"} {
		if err := ValidateHost(value); err != nil {
			t.Errorf("valid host %q: %v", value, err)
		}
	}
	for _, value := range []string{"", "https://cdn.example.test/path", "cdn.example:443", "bad\nname", "bad\x00name", "bad\tname", "bad name", "a..b", "-a.example", "a-.example", "[::1]", "fe80::1%eth0", "user@host", strings.Repeat("a", 64) + ".example", "host/path"} {
		if ValidateHost(value) == nil {
			t.Errorf("invalid host accepted: %q", value)
		}
	}
}
