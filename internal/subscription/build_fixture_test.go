package subscription

import "time"

// Test fixtures express strict/available construction tersely. Production uses
// the single Compile/CompileCached boundary with an explicit mode.
func Build(c Config, runtime Runtime, local map[string][]Node, now time.Time) (Snapshot, map[string]Preview, error) {
	return Compile(c, runtime, local, now, Strict)
}

func BuildAvailable(c Config, runtime Runtime, local map[string][]Node, now time.Time) (Snapshot, map[string]Preview, error) {
	return Compile(c, runtime, local, now, Available)
}
