package accounting

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func BenchmarkCredentialPolling100(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "control.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	if err = s.SaveAccount(b.Context(), domain.Account{ID: "account", Name: "account"}); err != nil {
		b.Fatal(err)
	}
	var credentials []domain.Credential
	for i := 0; i < 100; i++ {
		credentials = append(credentials, domain.Credential{ID: fmt.Sprint("c", i), AccountID: "account", RuntimeID: "normal", InboundID: fmt.Sprint("in", i), Protocol: "vless", Generation: 1, UUID: fmt.Sprintf("aabbccdd-1122-4333-8444-%012d", i)})
	}
	if err = s.BindBatch(b.Context(), credentials); err != nil {
		b.Fatal(err)
	}
	b.Run("Version", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := s.CredentialPublication(b.Context(), "normal"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Snapshot", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := s.CredentialSnapshot(b.Context(), "normal"); err != nil {
				b.Fatal(err)
			}
		}
	})
}
