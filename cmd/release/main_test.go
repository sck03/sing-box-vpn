package main

import (
	"path/filepath"
	"testing"
)

// Exercise the real checkout so removing or moving a packaged source cannot
// leave a stale archive entry that fails only after both architectures build.
func TestReleaseSourcesMatchCheckout(t *testing.T) {
	if err := validateReleaseSources(filepath.Join("..", "..")); err != nil {
		t.Fatal(err)
	}
}
