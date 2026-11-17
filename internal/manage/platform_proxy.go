package manage

import (
	"net/netip"
	"slices"
	"strings"
)

// Read from the trusted end of the chain; never accept a client-supplied prefix.
func forwardedPeer(header, fallback string, trusted []netip.Prefix) string {
	parts := strings.Split(header, ",")
	if len(parts) > 32 {
		return fallback
	}
	for i := len(parts) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			return fallback
		}
		ip = ip.Unmap()
		if !slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(ip) }) {
			return ip.String()
		}
	}
	return fallback
}
