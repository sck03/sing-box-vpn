package manage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// Artifact validation is deliberately separate from node connectivity. A
// successful sing-box probe cannot certify a different client's import format.
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
	switch template.Client {
	case "sing-box":
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
	case "mihomo", "openclash":
		result.Engine, result.Scope = "mihomo", "core-config"
		binary := mihomoBinary()
		if binary == "" {
			if os.Getenv("SBVPN_REQUIRE_CLIENT_CHECK") == "1" {
				return result, fmt.Errorf("Mihomo validator is required")
			}
			result.State = "unavailable"
			return result, nil
		}
		validationHome := os.Getenv("MIHOMO_HOME")
		identity, err := coreIdentity(binary)
		if err != nil {
			return result, err
		}
		key := "mihomo-" + sub.Digest([]byte(identity+"\n"+template.Mode+"\n"+validationHome+"\n"+sub.Digest(artifact.Data)))
		if validatedConfig(root, key) {
			result.State, result.Cached = "passed", true
			return result, nil
		}
		stage, err := os.MkdirTemp(root, ".client-check-")
		if err != nil {
			return result, err
		}
		defer os.RemoveAll(stage)
		path := filepath.Join(stage, "config.json")
		if err = os.WriteFile(path, artifact.Data, 0600); err != nil {
			return result, err
		}
		child, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if validationHome == "" {
			validationHome = stage
		}
		// Do not include core output: a rejected config may contain credentials.
		if err = exec.CommandContext(child, binary, "-t", "-d", validationHome, "-f", path).Run(); err != nil {
			return result, fmt.Errorf("Mihomo rejected the generated artifact: %w", err)
		}
		rememberValidation(root, key)
		result.State = "passed"
		return result, nil
	case "uri", "base64", "shadowrocket", "surge", "surge-mac", "surfboard", "loon", "quantumult-x":
		if template.Mode == "rules" {
			return result, nil
		}
		format := template.Client
		if format == "shadowrocket" {
			format = "base64"
		}
		if format == "surge-mac" || format == "surfboard" {
			format = "surge"
		}
		if _, err := sub.Parse(artifact.Data, format, "verification"); err != nil {
			return result, fmt.Errorf("generated output failed parser validation: %w", err)
		}
		result.State, result.Scope = "passed", "parser"
		return result, nil
	default:
		return result, nil
	}
}

func mihomoBinary() string {
	if binary := os.Getenv("MIHOMO_BINARY"); binary != "" {
		return binary
	}
	binary, _ := exec.LookPath("mihomo")
	return binary
}

// Parser validation depends only on the artifact and parser/compiler versions,
// which already participate in the account compilation cache key. Runtime core
// checks retain their binary and external-file dependency validation.
func parserValidatedClient(client string) bool {
	switch client {
	case "uri", "base64", "shadowrocket", "surge", "surge-mac", "surfboard", "loon", "quantumult-x":
		return true
	default:
		return false
	}
}
