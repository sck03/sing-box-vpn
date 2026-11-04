package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type cleanupFixture struct {
	namespace, link, forward, nat bool
	busy                          bool
	pids, killed                  []string
}

func (f *cleanupFixture) run(_ context.Context, name string, args ...string) ([]byte, error) {
	switch name + " " + strings.Join(args, " ") {
	case "ip netns list":
		if f.namespace {
			return []byte(namespace + " (id: 0)\n"), nil
		}
		return nil, nil
	case "ip netns pids " + namespace:
		return []byte(strings.Join(f.pids, "\n")), nil
	case "ip netns del " + namespace:
		if len(f.pids) > 0 {
			return nil, errors.New("namespace still has orphaned processes")
		}
		f.namespace = false
	case "ip link del " + hostVeth:
		f.link = false
	case "ip -j link show":
		if f.link {
			return []byte(`[{"ifname":"` + hostVeth + `"}]`), nil
		}
		return []byte("[]"), nil
	case "iptables -w 5 -S":
		if f.forward {
			return []byte("-N YGVG_FWD\n"), nil
		}
		return nil, nil
	case "iptables -w 5 -t nat -S", "iptables -t nat -S":
		if f.nat {
			return []byte("-N YGVG_NAT\n"), nil
		}
		return nil, nil
	case "iptables -w 5 -X YGVG_FWD":
		if f.busy {
			return nil, errors.New("firewall lock is busy")
		}
		f.forward = false
	case "iptables -w 5 -t nat -X YGVG_NAT":
		f.nat = false
	default:
		if name == "kill" {
			if len(args) < 3 || args[0] != "-KILL" || args[1] != "--" || !reflect.DeepEqual(args[2:], f.pids) {
				return nil, errors.New("cleanup tried to kill unrelated processes")
			}
			f.killed = append(f.killed, f.pids...)
			f.pids = nil
		}
	}
	return nil, nil
}

func ownedBackend(t *testing.T, fixture *cleanupFixture) (*backend, string) {
	t.Helper()
	c := auditConfig(t)
	owner := ownerState{Owner: ownerID, Subnet: "10.231.254.0/30", ForwardBefore: "1"}
	path := filepath.Join(c.DataDir, "namespace-owner.json")
	if err := atomicJSON(path, owner); err != nil {
		t.Fatal(err)
	}
	return &backend{cfg: c, owner: owner, claimed: true, runCommand: fixture.run}, path
}

func TestCleanupRetainsOwnershipUntilRetrySucceeds(t *testing.T) {
	fixture := &cleanupFixture{namespace: true, link: true, forward: true, nat: true, busy: true}
	b, marker := ownedBackend(t, fixture)
	b.stop()
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("failed cleanup lost the ownership record", err)
	}
	if !b.claimed || !fixture.forward {
		t.Fatal("failed cleanup was reported as complete")
	}
	fixture.busy = false
	b.stop()
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) || b.claimed || fixture.forward {
		t.Fatal("cleanup retry did not finish", err)
	}
}

func TestCleanupKillsOnlyOrphansInsideOwnedNamespace(t *testing.T) {
	fixture := &cleanupFixture{namespace: true, link: true, forward: true, nat: true, pids: []string{"410001", "410002"}}
	b, marker := ownedBackend(t, fixture)
	b.stop()
	if !reflect.DeepEqual(fixture.killed, []string{"410001", "410002"}) || fixture.namespace || fixture.link {
		t.Fatal("orphaned workers or namespace remained", fixture)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed cleanup retained its marker", err)
	}
}

func TestCleanupNeverTouchesUnownedResources(t *testing.T) {
	b := &backend{runCommand: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("unowned resource cleanup executed a command")
		return nil, nil
	}}
	b.stop()
}

func TestCleanupRejectsUnsafeProcessIDs(t *testing.T) {
	for _, pid := range []string{"1", strconv.Itoa(os.Getpid()), "4294967295", "--"} {
		t.Run(pid, func(t *testing.T) {
			fixture := &cleanupFixture{namespace: true, link: true, forward: true, nat: true, pids: []string{pid}}
			b, marker := ownedBackend(t, fixture)
			b.stop()
			if len(fixture.killed) != 0 || !fixture.namespace || !b.claimed {
				t.Fatal("unsafe process inventory was used for cleanup", fixture)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatal("unsafe process inventory lost its recovery record", err)
			}
		})
	}
}

func TestCleanupRetainsOwnershipWhenInventoryUnavailable(t *testing.T) {
	fixture := &cleanupFixture{namespace: true, link: true, forward: true, nat: true}
	b, marker := ownedBackend(t, fixture)
	b.runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ip" && strings.Join(args, " ") == "netns list" {
			return nil, errors.New("namespace inventory unavailable")
		}
		return fixture.run(ctx, name, args...)
	}
	b.stop()
	if !b.claimed || !fixture.namespace {
		t.Fatal("failed inventory lookup unlinked the namespace")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("failed inventory lookup lost ownership", err)
	}
}
