package manage

import (
	"net"
	"strconv"
	"strings"
)

// Only historical fixture builders need an HTTP URL before public access setup.
func fixtureSubscriptionBase(s subscriptionSettings, host string) string {
	if s.PublicBaseURL != "" {
		return strings.TrimRight(s.PublicBaseURL, "/")
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(s.Port))
}
