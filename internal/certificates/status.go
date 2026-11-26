package certificates

import "time"

// CertificateStatus is the public, secret-free view of an asset and its uses.
type CertificateStatus struct {
	ID                 string     `json:"id"`
	Domain             string     `json:"domain"`
	Provider           string     `json:"provider"`
	Saved              bool       `json:"saved"`
	Applied            bool       `json:"applied"`
	Pending            bool       `json:"pending"`
	CanRenew           bool       `json:"can_renew"`
	RenewalUnavailable string     `json:"renewal_unavailable,omitempty"`
	UsedBy             []string   `json:"used_by"`
	Ready              bool       `json:"ready"`
	RenewalState       string     `json:"renewal_state"`
	DNSNames           []string   `json:"dns_names,omitempty"`
	NotBefore          *time.Time `json:"not_before,omitempty"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	DaysLeft           *int       `json:"days_left,omitempty"`
	Error              string     `json:"error,omitempty"`
}
