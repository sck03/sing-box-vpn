package domain

import "testing"

func TestTaskDetailAudience(t *testing.T) {
	for _, kind := range []string{"apply", "service", "gateway", "account", "subscription"} {
		a := TaskDetails(kind, "alice")
		if !a.Allows("admin", "root") || a.Allows("viewer", "alice") {
			t.Fatal(kind, a)
		}
		if got, want := a.Allows("operator", "alice"), kind == "apply" || kind == "service" || kind == "gateway"; got != want {
			t.Fatal(kind, got)
		}
		if got, want := a.Allows("operator", "bob"), kind == "service" || kind == "gateway"; got != want {
			t.Fatal(kind, got)
		}
	}
}
