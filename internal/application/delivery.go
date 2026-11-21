package application

type DeliveryOutcome struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	Persisted bool   `json:"persisted"`
}

type DeliveryResult struct {
	Outcome     string            `json:"outcome"`
	Selected    int               `json:"selected"`
	Items       []DeliveryOutcome `json:"items"`
	Interrupted bool              `json:"interrupted"`
}

func (r *DeliveryResult) Complete(err error) {
	r.Outcome = "passed"
	completed := 0
	for _, item := range r.Items {
		if (item.Status == "delivered" || item.Status == "unchanged") && item.Persisted {
			completed++
		}
	}
	if err != nil || r.Interrupted || completed < r.Selected {
		r.Outcome = "failed"
		if completed > 0 {
			r.Outcome = "partial"
		}
	}
}
