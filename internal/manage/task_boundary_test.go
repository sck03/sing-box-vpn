package manage

import (
	"net/http/httptest"
	"testing"
)

func TestPublicTaskRejectsSchedulingMetadata(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, session := platformFixture(t, root, core)
	handler := s.Handler(PanelOptions{PublicURL: "http://example.com"})
	for _, body := range []string{
		`{"operation":"subscription","automatic":true,"subscription":{"operation":"check","id":"missing"}}`,
		`{"operation":"service","service":"sb-core","value":"stop","scope":"normal"}`,
		`{"operation":"service","service":"sb-core","value":"stop","id":"ignored"}`,
		`{"operation":"subscription","subscription":{"operation":"deliver"}}`,
		`{"operation":"subscription","subscription":{"operation":"check","id":"output"}}`,
		`{"operation":"subscription","subscription":{"operation":"check","id":"output","probe_url":"file:///tmp/probe"}}`,
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, panelRequest("POST", "/api/tasks", []byte(body), session))
		if response.Code != 400 {
			t.Fatalf("%d %s", response.Code, response.Body.String())
		}
	}
	counts, err := s.Store.TaskCounts(t.Context())
	if err != nil || counts.Active != 0 {
		t.Fatal(counts, err)
	}
	if _, err := s.queueAutomaticSubscription(t.Context(), "check"); err == nil {
		t.Fatal("scheduled probe accepted")
	}
}
