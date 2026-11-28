package domain

// CredentialPublication separates durable mutations, generated downloads and
// credentials loaded by a running core. Mutations advance Revision in the same
// transaction, so a crash cannot lose the need to reconcile.
type CredentialPublication struct {
	Runtime   string `json:"runtime"`
	Revision  int64  `json:"revision"`
	Published int64  `json:"published"`
	Loaded    int64  `json:"loaded"`
}
