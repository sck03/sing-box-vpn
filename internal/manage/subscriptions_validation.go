package manage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// Client formats are checked by pure subscription parsers. Only sing-box
// configurations use the server's existing core; client acceptance belongs in CI.
func validateSubscriptionArtifact(ctx context.Context, root, core string, template sub.Template, artifact sub.Artifact) (result sub.Validation, failure error) {
	result = sub.Validation{State: "encoded", Engine: template.Client, Scope: "encoder"}
	defer func() {
		if failure != nil {
			result.State = "failed"
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if template.Client != "sing-box" {
		return sub.ValidateArtifact(template, artifact)
	}
	result.Scope = "core-config"
	if core == "" {
		if os.Getenv("SBVPN_REQUIRE_CLIENT_CHECK") == "1" {
			return result, fmt.Errorf("sing-box validator is required")
		}
		result.State = "unavailable"
		return result, nil
	}
	var config Object
	if err := json.Unmarshal(artifact.Data, &config); err != nil {
		return result, err
	}
	if err := CheckJSON(ctx, core, root, config); err != nil {
		return result, err
	}
	result.State = "passed"
	return result, nil
}
