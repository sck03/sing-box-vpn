package accounting

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestBindingKeysSelectsOnlyRequestedMembership(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, id := range []string{"alice", "bob"} {
		if err := store.SaveAccount(t.Context(), domain.Account{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	for i, item := range [][3]string{{"alice", "normal", "one"}, {"alice", "normal", "two"}, {"bob", "normal", "one"}, {"alice", "vpngate", "vpn"}} {
		credential := domain.Credential{ID: fmt.Sprint("credential-", i), AccountID: item[0], RuntimeID: item[1], InboundID: item[2], Protocol: "vless", Generation: 1, UUID: fmt.Sprintf("aabbccdd-1122-4333-8444-%012d", i)}
		if err := store.BindBatch(t.Context(), []domain.Credential{credential}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, runtime      string
		accounts, inbounds []string
		want               map[[2]string]bool
	}{
		{"selected", "normal", []string{"alice"}, []string{"one"}, map[[2]string]bool{{"alice", "one"}: true}},
		{"batch", "normal", []string{"alice", "bob"}, []string{"one"}, map[[2]string]bool{{"alice", "one"}: true, {"bob", "one"}: true}},
		{"other-runtime", "normal", []string{"alice"}, []string{"vpn"}, map[[2]string]bool{}},
		{"empty-accounts", "normal", nil, []string{"one"}, map[[2]string]bool{}},
		{"empty-inbounds", "normal", []string{"alice"}, nil, map[[2]string]bool{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.BindingKeys(t.Context(), tc.runtime, tc.accounts, tc.inbounds)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

// Compare the former full credential scan with the selected membership query
// on the same durable SQLite database. Setup is outside the measured interval.
func BenchmarkBindingKeyLookup(b *testing.B) {
	for _, size := range []int{100, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			store, err := Open(filepath.Join(b.TempDir(), "control.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer store.Close()
			tx, err := store.db.BeginTx(b.Context(), nil)
			if err != nil {
				b.Fatal(err)
			}
			defer tx.Rollback()
			for i := range size {
				id := fmt.Sprint("account-", i)
				if _, err := tx.Exec(`INSERT INTO accounts(id,name,disabled,revision,created) VALUES(?,?,0,1,0)`, id, id); err != nil {
					b.Fatal(err)
				}
				c := domain.Credential{ID: id, AccountID: id, RuntimeID: "normal", InboundID: "selected", Protocol: "vless", Generation: 1, UUID: fmt.Sprintf("aabbccdd-1122-4333-8444-%012d", i)}
				secret, err := json.Marshal(c)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := tx.Exec(`INSERT INTO credentials(id,account,runtime,inbound,protocol,generation,secret) VALUES(?,?,'normal','selected','vless',1,?)`, id, id, secret); err != nil {
					b.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				b.Fatal(err)
			}
			for _, mode := range []string{"full-credentials", "binding-keys"} {
				b.Run(mode, func(b *testing.B) {
					accounts, inbounds := []string{"account-0"}, []string{"selected"}
					b.ReportAllocs()
					for b.Loop() {
						var keys map[[2]string]bool
						if mode == "full-credentials" {
							credentials, err := store.Credentials(b.Context(), "normal")
							if err != nil {
								b.Fatal(err)
							}
							keys = make(map[[2]string]bool, len(credentials))
							for _, c := range credentials {
								keys[[2]string{c.AccountID, c.InboundID}] = true
							}
						} else {
							var err error
							keys, err = store.BindingKeys(b.Context(), "normal", accounts, inbounds)
							if err != nil {
								b.Fatal(err)
							}
						}
						if !keys[[2]string{"account-0", "selected"}] {
							b.Fatal("binding missing")
						}
					}
				})
			}
		})
	}
}
