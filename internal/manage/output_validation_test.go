package manage

import "testing"

func TestHeavyRequestsLeaveStatusCapacity(t *testing.T) {
	for _, path := range []string{"/api/config", "/api/config/preview", "/api/config/preview-output", "/api/accounts/export", "/api/grants", "/api/restore"} {
		if !expensivePanelRequest(path, "POST") {
			t.Fatal(path)
		}
	}
	for _, path := range []string{"/api/config", "/api/overview", "/api/accounts/outputs", "/api/events", "/healthz"} {
		if expensivePanelRequest(path, "GET") {
			t.Fatal(path)
		}
	}
}
