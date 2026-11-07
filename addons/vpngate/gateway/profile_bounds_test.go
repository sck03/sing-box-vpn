package main

import (
	"strings"
	"testing"
)

func TestProfileDirectiveArgumentBounds(t *testing.T) {
	for _, directive := range []string{
		"client extra",
		"proto udp extra",
		"remote 203.0.113.5 1194 udp extra",
		"remote " + strings.Repeat("x ", 1<<18),
	} {
		if _, _, _, err := validateProfile(directive+"\n"+safeTestProfile, "203.0.113.5"); err == nil {
			t.Fatal("accepted excess directive arguments")
		}
	}
	profile := strings.Replace(safeTestProfile, "remote 203.0.113.5 1194", "remote\t203.0.113.5 1194 tcp-client", 1)
	if _, port, network, err := validateProfile(profile, "203.0.113.5"); err != nil || port != 1194 || network != "tcp" {
		t.Fatalf("valid three-argument remote rejected: %d %s %v", port, network, err)
	}
}

func BenchmarkProfileValidation(b *testing.B) {
	for name, profile := range map[string]string{
		"Valid":           safeTestProfile,
		"ExcessArguments": "remote " + strings.Repeat("x ", 1<<18) + "\n" + safeTestProfile,
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _, _, err := validateProfile(profile, "203.0.113.5")
				if (err == nil) != (name == "Valid") {
					b.Fatal("unexpected validation result", err)
				}
			}
		})
	}
}
