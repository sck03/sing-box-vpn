package manage

import (
	"context"
	"errors"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

var errProbeUnsupported = errors.New("sing-box does not support this node's connection settings")

func checkSubscriptionNode(ctx context.Context, root, core, endpoint string, node sub.Node) subscriptionCheck {
	result := subscriptionCheck{NodeID: node.ID, Name: node.Name, Engine: "sing-box", State: "passed"}
	start := time.Now()
	err := ctx.Err()
	if err == nil {
		err = probeSubscriptionNode(ctx, root, core, endpoint, node)
	}
	result.Milliseconds = time.Since(start).Milliseconds()
	result.Success = err == nil
	if err != nil {
		result.Error = err.Error()
		result.State = "failed"
		if ctx.Err() != nil {
			result.State = "cancelled"
		} else if errors.Is(err, errProbeUnsupported) {
			result.State = "unsupported"
		}
	}
	return result
}

func summarizeSubscriptionChecks(results []subscriptionCheck) (passed, skipped int, outcome string) {
	for _, result := range results {
		if result.Success {
			passed++
		} else if result.State == "unsupported" {
			skipped++
		}
	}
	switch {
	case skipped == len(results):
		outcome = "not_run"
	case passed == len(results):
		outcome = "passed"
	case passed > 0 || skipped > 0:
		outcome = "partial"
	default:
		outcome = "failed"
	}
	return
}
