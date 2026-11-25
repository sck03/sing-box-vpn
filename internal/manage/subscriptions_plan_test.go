package manage

import (
	"net/http/httptest"

	"strings"
	"testing"
)

func TestPreviewSummaryAndHTTPConflict(t *testing.T) {
	root, core, _ := appFixture(t)
	s, c, session := platformFixture(t, root, core)
	result, err := s.PreviewConfiguration(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(mustJSON(t, result))
	for _, secret := range []string{fixtureCredential("vless").UUID, fixtureCredential("tuic").Password} {
		if strings.Contains(encoded, secret) {
			t.Fatal("preview leaks credential")
		}
	}
	handler := s.Handler(PanelOptions{PublicURL: "http://example.com"})
	req := panelRequest("POST", "/api/config", mustJSON(t, Object{"config": c, "revision": 0}), session)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 409 {
		t.Fatal("revision conflict status", w.Code, w.Body.String())
	}
}
