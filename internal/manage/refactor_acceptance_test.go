package manage

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/runtimeconfig"
)

func TestPreviewAndSaveRejectUnsupportedManagedTransport(t *testing.T) {
	root, core, _ := appFixture(t)
	s, config, _ := platformFixture(t, root, core)
	config.App.Advanced.DNS = Object{"servers": []Object{{"type": "local", "tag": "dns-local"}, {"type": "quic", "tag": "dns-audit", "server": "1.1.1.1"}}, "final": "dns-audit"}
	_, previewErr := s.PreviewConfiguration(t.Context(), config)
	_, saveErr := s.QueueApply(t.Context(), config, 1, "test")
	for _, err := range []error{previewErr, saveErr} {
		var field *runtimeconfig.FieldError
		if !errors.As(err, &field) || field.Field != "dns.servers[1].type" {
			t.Fatalf("unsupported capability accepted: %v", err)
		}
	}
	state, err := s.Store.State(t.Context())
	if err != nil || state.Revision != 1 {
		t.Fatal("rejected candidate mutated authority", state, err)
	}
}

func TestManagedNodeHasNoSharedAuthenticationMaterial(t *testing.T) {
	_, _, app := appFixture(t)
	raw, err := json.Marshal(app)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"uuid"`)) || bytes.Contains(raw, []byte(`"password"`)) {
		t.Fatal("node document contains shared credentials")
	}
	for _, in := range app.Inbounds() {
		if len(objects(in["users"])) != 0 {
			t.Fatal("unbound listener has users")
		}
	}
}

func TestPanelAssetsCompressionAndRevalidation(t *testing.T) {
	get := func(encoding, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/panel/app.js", nil)
		r.Header.Set("Accept-Encoding", encoding)
		r.Header.Set("If-None-Match", etag)
		w := httptest.NewRecorder()
		servePanelAsset(w, r, "panel/app.js")
		return w
	}
	plain, compressed := get("gzip;q=0", ""), get("br, gzip", "")
	reader, err := gzip.NewReader(bytes.NewReader(compressed.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(decoded, plain.Body.Bytes()) || compressed.Body.Len() >= plain.Body.Len() {
		t.Fatal("incorrect compression", err)
	}
	if plain.Header().Get("ETag") == compressed.Header().Get("ETag") {
		t.Fatal("strong ETags must distinguish encodings")
	}
	if get("gzip", compressed.Header().Get("ETag")).Code != 304 {
		t.Fatal("compressed revalidation failed")
	}
	if get("gzip;q=0, *;q=1", "").Header().Get("Content-Encoding") != "" {
		t.Fatal("explicitly forbidden encoding selected")
	}
}

func TestRuntimeDiagnosticsBoundedAndRedacted(t *testing.T) {
	result := validationDiagnostic([]byte("unknown transport quic password=top-secret\n"+strings.Repeat("x", 9000)), []byte(`{"password":"top-secret"}`))
	if !strings.Contains(result, "unknown transport quic") || strings.Contains(result, "top-secret") || len([]rune(result)) > 4100 {
		t.Fatal("unsafe or missing diagnostic")
	}
}
