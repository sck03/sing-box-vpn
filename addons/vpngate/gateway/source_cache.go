package main

import (
	"path/filepath"
	"slices"

	"github.com/sck03/sing-box-vpn/internal/manage"
)

// Source identity travels with both the candidate list and the last verified
// node. A late write from a cancelled refresh cannot become another source's cache.
type sourceCache[T any] struct {
	Sources []string `json:"sources"`
	Value   T        `json:"value"`
}

func loadSourceCache[T any](c Config, name string) T {
	var cached sourceCache[T]
	found, err := manage.ReadJSON(filepath.Join(c.DataDir, name), &cached)
	if found && err == nil && slices.Equal(cached.Sources, c.Sources) {
		return cached.Value
	}
	var empty T
	return empty
}

func saveSourceCache[T any](c Config, name string, value T) error {
	return atomicJSON(filepath.Join(c.DataDir, name), sourceCache[T]{Sources: c.Sources, Value: value})
}
