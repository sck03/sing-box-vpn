package application

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRefreshReportFitsTaskBudgetAndPreservesPublicationOutcome(t *testing.T) {
	r := RefreshResult{Published: true, Outputs: 1, ExpectedOutputs: 1, Checked: 384}
	for range 384 {
		r.Failures = append(r.Failures, RefreshFailure{ID: "template:" + strings.Repeat("a", 64), Error: strings.Repeat("<\x00界", 2048), LastSuccess: time.Now()})
	}
	r.Complete()
	data, err := json.Marshal(r)
	if err != nil || len(data) > 256<<10 || len(r.Failures) != 384 || r.Outcome != "cached" {
		t.Fatal(len(data), r.Outcome, err)
	}
	r.Deferred = 1
	r.Complete()
	if r.Outcome != "partial" {
		t.Fatal(r.Outcome)
	}
	r.Published = false
	r.Complete()
	if r.Outcome != "failed" {
		t.Fatal(r.Outcome)
	}
}
