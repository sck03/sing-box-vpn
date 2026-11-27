package domain

const (
	ControlLane      = "control"
	SubscriptionLane = "subscription"
	ProbeLane        = "probe"
)

// TaskAudience is a transport-independent capability description. Actor grants
// apply only to operators; viewers never gain access to task details.
type TaskAudience struct {
	Operators bool   `json:"operators"`
	Actor     string `json:"actor,omitempty"`
}

func TaskDetails(kind, owner string) TaskAudience {
	return TaskAudience{Operators: kind == "service" || kind == "gateway", Actor: taskOwner(kind, owner)}
}

func taskOwner(kind, owner string) string {
	if kind == "apply" {
		return owner
	}
	return ""
}

func (a TaskAudience) Allows(role, actor string) bool {
	return role == "admin" || role == "operator" && (a.Operators || a.Actor != "" && a.Actor == actor)
}
