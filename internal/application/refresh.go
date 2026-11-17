package application

import "time"

type RefreshFailure struct {
	ID          string    `json:"id"`
	Error       string    `json:"error"`
	LastSuccess time.Time `json:"last_success"`
}

type RefreshResult struct {
	Outcome         string           `json:"outcome"`
	Checked         int              `json:"checked"`
	Updated         int              `json:"updated"`
	Outputs         int              `json:"outputs"`
	ExpectedOutputs int              `json:"expected_outputs"`
	Failures        []RefreshFailure `json:"failures"`
}

func (r *RefreshResult) Complete() {
	r.Outcome = "passed"
	if len(r.Failures) > 0 {
		r.Outcome = "partial"
		if r.Updated == 0 {
			r.Outcome = "cached"
		}
	}
	if len(r.Failures) > 0 && r.ExpectedOutputs > 0 && r.Outputs == 0 {
		r.Outcome = "failed"
	}
}
