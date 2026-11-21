package application

import (
	"strings"
	"time"
	"unicode"
)

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
	Deferred        int              `json:"deferred"`
	Published       bool             `json:"published"`
	Interrupted     bool             `json:"interrupted"`
}

func (r *RefreshResult) Complete() {
	// At most 384 configured inputs can fail. Keep the complete list under the
	// task store's 256 KiB budget even for HTML-escaped/untrusted diagnostics.
	for i := range r.Failures {
		var text strings.Builder
		count := 0
		for _, ch := range r.Failures[i].Error {
			if count == 64 {
				text.WriteRune('…')
				break
			}
			if unicode.IsControl(ch) {
				ch = ' '
			}
			text.WriteRune(ch)
			count++
		}
		r.Failures[i].Error = text.String()
	}
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
	if !r.Published {
		r.Outcome = "failed"
	} else if r.Deferred > 0 || r.Interrupted {
		r.Outcome = "partial"
	}
}
