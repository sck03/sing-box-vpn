package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupRejectsInvalidOwnershipBeforeCommands(t *testing.T) {
	for _, tc := range []struct {
		name, data string
	}{
		{"directory", ""},
		{"null", "null"},
		{"malformed", "{"},
		{"foreign", `{"owner":"other","subnet":"10.231.254.0/30","forward_before":"1"}`},
		{"missing-subnet", `{"owner":"` + ownerID + `","forward_before":"1"}`},
		{"public-subnet", `{"owner":"` + ownerID + `","subnet":"8.8.8.0/30","forward_before":"1"}`},
		{"broad-subnet", `{"owner":"` + ownerID + `","subnet":"10.0.0.0/8","forward_before":"1"}`},
		{"host-bits", `{"owner":"` + ownerID + `","subnet":"10.231.254.1/30","forward_before":"1"}`},
		{"missing-forward-state", `{"owner":"` + ownerID + `","subnet":"10.231.254.0/30"}`},
		{"invalid-forward-state", `{"owner":"` + ownerID + `","subnet":"10.231.254.0/30","forward_before":"2"}`},
		{"oversized", `{"owner":"` + ownerID + `","subnet":"10.231.254.0/30","forward_before":"1"}` + strings.Repeat(" ", 16<<20)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := auditConfig(t)
			path := filepath.Join(cfg.DataDir, "namespace-owner.json")
			if tc.name == "directory" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			commands := 0
			fixture := &cleanupFixture{}
			b := &backend{cfg: cfg, runCommand: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				commands++
				return fixture.run(ctx, name, args...)
			}}
			if err := b.setup(context.Background(), Node{}); err == nil {
				t.Fatal("invalid ownership was accepted")
			}
			if commands != 0 || b.claimed {
				t.Fatalf("invalid ownership reached resource management: commands=%d, claimed=%v", commands, b.claimed)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("invalid marker was removed", err)
			}
		})
	}
}

func TestRecoverOwnership(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		b := &backend{cfg: auditConfig(t), runCommand: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("absent record caused resource cleanup")
			return nil, nil
		}}
		if err := b.recoverOwnership(); err != nil || b.claimed {
			t.Fatal("absent ownership was not a no-op", err)
		}
	})
	for _, busy := range []bool{false, true} {
		name := "complete"
		if busy {
			name = "retry"
		}
		t.Run(name, func(t *testing.T) {
			fixture := &cleanupFixture{namespace: true, link: true, forward: true, nat: true, busy: busy}
			b, marker := ownedBackend(t, fixture)
			b.owner, b.claimed = ownerState{}, false // A new process must read the record.
			err := b.recoverOwnership()
			if (err != nil) != busy || b.claimed != busy {
				t.Fatalf("recovery error=%v, claimed=%v, busy=%v", err, b.claimed, busy)
			}
			if _, err := os.Stat(marker); os.IsNotExist(err) == busy {
				t.Fatal("ownership record was not retained according to recovery result", err)
			}
		})
	}
}
