package controlstore

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestDesiredStateKeepsPendingAndAppliedRevisions(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Initialize(t.Context(), []byte(`{"value":1}`)); err != nil {
		t.Fatal(err)
	}
	desired := []byte(`{"value":2}`)
	if _, err = s.Queue(t.Context(), "apply", "admin", []byte(`{}`), 1, desired); err != nil {
		t.Fatal(err)
	}
	state, err := s.DesiredState(t.Context())
	if err != nil || state.Revision != 2 || state.AppliedRevision != 1 || !bytes.Equal(state.Config, desired) {
		t.Fatalf("pending configuration and revisions: %+v, %v", state, err)
	}
	applied, err := s.AppliedConfig(t.Context())
	if err != nil || string(applied) != `{"value":1}` {
		t.Fatalf("applied configuration changed before publication: %s, %v", applied, err)
	}
}

func BenchmarkConfigurationRead(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "control.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	data := append([]byte(`{"content":"`), bytes.Repeat([]byte("x"), 1<<20)...)
	data = append(data, []byte(`"}`)...)
	if err = s.Initialize(b.Context(), data); err != nil {
		b.Fatal(err)
	}
	for _, mode := range []string{"full", "desired", "revisions"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var err error
				switch mode {
				case "full":
					_, err = s.State(b.Context())
				case "desired":
					_, err = s.DesiredState(b.Context())
				case "revisions":
					_, err = s.Revisions(b.Context())
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
