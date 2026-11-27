package domain

// BoundInbound is membership only; configuration validation needs no secrets.
type BoundInbound struct {
	ID       string
	Protocol string
}
