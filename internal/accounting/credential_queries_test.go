package accounting

import (
	"path/filepath"
	"testing"
)

// Compare the old runtime-wide secret read with a single membership lookup
// against identical rows. Only the indexed query is used by the worker.
func BenchmarkCredentialMembership(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "control.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	_, err = s.db.Exec(`INSERT INTO accounts(id,name,disabled,revision,created) VALUES('a','a',0,1,0);
	WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<1000)
	INSERT INTO credentials(id,account,runtime,inbound,protocol,generation,secret)
	SELECT 'c'||i,'a','normal','in'||i,'vless',1,json_object('id','c'||i,'uuid','u'||i) FROM n`)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("runtime-list", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			cs, err := s.Credentials(b.Context(), "normal")
			if err != nil || len(cs) != 1000 {
				b.Fatal(err)
			}
		}
	})
	b.Run("indexed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			ok, err := s.HasCredential(b.Context(), "normal", "c500")
			if err != nil || !ok {
				b.Fatal(err)
			}
		}
	})
}
