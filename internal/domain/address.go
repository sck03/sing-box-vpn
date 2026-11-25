package domain

import (
	"errors"
	"net/netip"
	"strings"
)

// ValidateHost accepts a bare IP or DNS name, never a URL, port or IPv6 zone.
// Configuration and subscription publication share this boundary.
func ValidateHost(value string) error {
	if ip, err := netip.ParseAddr(value); err == nil && ip.Zone() == "" {
		return nil
	}
	return ValidateDomain(value)
}

func ValidateDomain(value string) error {
	if len(value) == 0 || len(value) > 253 {
		return errors.New("expected a DNS name or bare IP address")
	}
	for label := range strings.SplitSeq(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("invalid DNS label")
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return errors.New("expected a DNS name or bare IP address without scheme, port or path")
			}
		}
	}
	return nil
}

type FieldError struct{ Field, Message string }

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }
