package certificates

// CertificateAsset is the registry entry; PEM files are the sole material source.
type CertificateAsset struct {
	ID          string `json:"id"`
	Domain      string `json:"domain"`
	Certificate string `json:"certificate"`
	Key         string `json:"key"`
	Provider    string `json:"provider"`
	Email       string `json:"email,omitempty"`
}
