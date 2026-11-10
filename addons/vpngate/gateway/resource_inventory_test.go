package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/command"
)

func TestSetupInventoryFailureNeverClaimsResources(t *testing.T) {
	for _, resource := range []string{"namespace", "link", "filter", "nat"} {
		for _, failure := range []error{errors.New("permission denied"), context.DeadlineExceeded, command.ErrOutputLimit} {
			t.Run(resource+"/"+failure.Error(), func(t *testing.T) {
				cfg := auditConfig(t)
				mutations := 0
				b := &backend{cfg: cfg, runCommand: func(_ context.Context, name string, args ...string) ([]byte, error) {
					query := name + " " + strings.Join(args, " ")
					switch query {
					case "ip netns list":
						if resource == "namespace" {
							return nil, failure
						}
						return nil, nil
					case "ip -j link show":
						if resource == "link" {
							return nil, failure
						}
						return []byte("[]"), nil
					case "iptables -w 5 -S":
						if resource == "filter" {
							return nil, failure
						}
						return nil, nil
					case "iptables -w 5 -t nat -S":
						if resource == "nat" {
							return nil, failure
						}
						return nil, nil
					case "ip -j -4 route show table all":
						return []byte("[]"), nil
					case "sysctl -n net.ipv4.ip_forward":
						return []byte("1"), nil
					default:
						mutations++
						return nil, errors.New("unexpected resource mutation")
					}
				}}
				if err := b.setup(context.Background(), Node{}); !errors.Is(err, failure) {
					t.Errorf("inventory error lost: %v", err)
				}
				if mutations != 0 || b.claimed {
					t.Errorf("failed inventory reached resource management: mutations=%d, claimed=%v", mutations, b.claimed)
				}
				if _, err := os.Stat(filepath.Join(cfg.DataDir, "namespace-owner.json")); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("failed inventory left ownership record: %v", err)
				}
			})
		}
	}
}

func TestSetupInventoryOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, query, output string
		allowed             bool
	}{
		{"namespace", "ip netns list", namespace + " (id: 0)\n", false},
		{"veth", "ip -j link show", `[{"ifname":"ygvg-host"}]`, false},
		{"filter", "iptables -w 5 -S", "-N YGVG_FWD\n", false},
		{"nat", "iptables -w 5 -t nat -S", "-N YGVG_NAT\n", false},
		{"malformed-links", "ip -j link show", "[{", false},
		{"null-links", "ip -j link show", "null", false},
		{"nameless-links", "ip -j link show", "[{}]", false},
		{"absent", "", "", true},
		{"similar-namespace", "ip netns list", "ygvg0-other (id: 0)\n", true},
		{"similar-veth", "ip -j link show", `[{"ifname":"ygvg-host-other"}]`, true},
		{"similar-chain", "iptables -w 5 -S", "-N YGVG_FWD_OTHER\n", true},
		{"comment", "iptables -w 5 -S", "-A INPUT -m comment --comment \"YGVG_FWD\" -j ACCEPT\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := auditConfig(t)
			mutations := 0
			b := &backend{cfg: cfg, runCommand: func(_ context.Context, name string, args ...string) ([]byte, error) {
				query := name + " " + strings.Join(args, " ")
				if query == tc.query {
					return []byte(tc.output), nil
				}
				switch query {
				case "ip netns list", "iptables -w 5 -S", "iptables -w 5 -t nat -S":
					return nil, nil
				case "ip -j link show", "ip -j -4 route show table all":
					return []byte("[]"), nil
				case "sysctl -n net.ipv4.ip_forward":
					return []byte("1"), nil
				default:
					mutations++
					return nil, nil
				}
			}}
			err := b.setup(context.Background(), Node{})
			if (err == nil) != tc.allowed || b.claimed != tc.allowed || (mutations > 0) != tc.allowed {
				t.Fatalf("allowed=%v, error=%v, claimed=%v, mutations=%d", tc.allowed, err, b.claimed, mutations)
			}
			_, err = os.Stat(filepath.Join(cfg.DataDir, "namespace-owner.json"))
			if tc.allowed && err != nil || !tc.allowed && !errors.Is(err, os.ErrNotExist) {
				t.Fatal("ownership record does not match verified inventory", err)
			}
		})
	}
}

func TestCleanupRetainsOwnershipOnInventoryFailure(t *testing.T) {
	for _, query := range []string{"ip -j link show", "iptables -w 5 -S", "iptables -w 5 -t nat -S"} {
		t.Run(query, func(t *testing.T) {
			fixture := &cleanupFixture{}
			b, marker := ownedBackend(t, fixture)
			failure := errors.New("inventory unavailable")
			b.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if name+" "+strings.Join(args, " ") == query {
					return nil, failure
				}
				return fixture.run(ctx, name, args...)
			}
			b.stop()
			if !errors.Is(b.cleanupErr, failure) || !b.claimed {
				t.Fatal("failed inventory was treated as complete cleanup", b.cleanupErr)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatal("failed inventory lost its recovery record", err)
			}
		})
	}
}

func TestCleanupIgnoresUnrelatedChainNamesAndComments(t *testing.T) {
	fixture := &cleanupFixture{}
	b, marker := ownedBackend(t, fixture)
	b.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch name + " " + strings.Join(args, " ") {
		case "iptables -w 5 -S":
			return []byte("-N YGVG_FWD_OTHER\n-A INPUT -m comment --comment \"YGVG_FWD\" -j ACCEPT\n"), nil
		case "iptables -w 5 -t nat -S":
			return []byte("-N YGVG_NAT_OTHER\n-A OUTPUT -m comment --comment \"YGVG_NAT\" -j ACCEPT\n"), nil
		}
		return fixture.run(ctx, name, args...)
	}
	b.stop()
	if b.cleanupErr != nil || b.claimed {
		t.Fatalf("unrelated rules prevented cleanup: %v", b.cleanupErr)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed cleanup retained ownership", err)
	}
}

func BenchmarkNamespaceListed(b *testing.B) {
	data := []byte(strings.Repeat("unrelated (id: 123)\n", 4096))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if namespaceListed(data) {
			b.Fatal("unrelated namespace matched")
		}
	}
}
