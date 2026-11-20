package manage

import (
	"path/filepath"
	"testing"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestSubscriptionValidationCacheTracksCompilationIdentity(t *testing.T) {
	root, core := t.TempDir(), coreBinary(t)
	config := sub.Config{
		Version:     sub.SchemaVersion,
		Sources:     []sub.Source{{ID: "source", Name: "source", Kind: "inline", Format: "uri", Content: "hysteria2://fixture@example.com:443#test"}},
		Collections: []sub.Collection{{ID: "nodes", Name: "nodes", Sources: []string{"source"}}},
		Templates:   []sub.Template{{ID: "client", Client: "sing-box", Version: 1, Mode: "nodes"}},
		Outputs:     []sub.Output{{ID: "output", Name: "output", Collection: "nodes", Template: "client"}},
	}
	_, previous, _, err := compilePreparedSubscriptions(t.Context(), root, core, config, sub.Runtime{}, nil, sub.Strict)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []bool{false, true} {
		if changed {
			// A compiler/input change must repeat semantic validation even when
			// both compilers happen to emit exactly the same configuration bytes.
			previous.InputDigests["output"] = "previous-compiler-identity"
		}
		if err := AtomicJSON(filepath.Join(root, subscriptionsSnapshot), previous); err != nil {
			t.Fatal(err)
		}
		_, current, _, err := compilePreparedSubscriptions(t.Context(), root, core, config, sub.Runtime{}, nil, sub.Strict)
		if err != nil {
			t.Fatal(err)
		}
		if current.Artifacts["output"].ETag != previous.Artifacts["output"].ETag {
			t.Fatal("fixture changed the output bytes")
		}
		if current.Validations["output"].Cached == changed {
			t.Fatalf("validation cache ignored compilation identity: changed=%v, cached=%v", changed, current.Validations["output"].Cached)
		}
	}
}
