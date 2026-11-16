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
func validateSubscriptionArtifact(ctx context.Context, root, core string, template sub.Template, artifact sub.Artifact) (string, error) {
	switch template.Client {
	case "sing-box":
		var config Object
		if err := json.Unmarshal(artifact.Data, &config); err != nil {
			return "", err
		}
		if err := CheckJSON(ctx, core, root, config); err != nil {
			return "", err
		}
		return "sing-box core-check", nil
	case "mihomo", "openclash":
		binary := os.Getenv("MIHOMO_BINARY")
		if binary == "" {
			binary, _ = exec.LookPath("mihomo")
		}
		if binary == "" {
			return "encoder-only; Mihomo validator unavailable", nil
		}
		validationHome := os.Getenv("MIHOMO_HOME")
		if template.Mode == "config" && template.Preset == "split" {
			if validationHome == "" {
				return "encoder-only; split validation requires MIHOMO_HOME with local Country.mmdb", nil
			}
			if _, err := os.Stat(filepath.Join(validationHome, "Country.mmdb")); err != nil {
				return "encoder-only; local Country.mmdb unavailable", nil
			}
		}
		stage, err := os.MkdirTemp(root, ".client-check-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(stage)
		path := filepath.Join(stage, "config.json")
		if err = os.WriteFile(path, artifact.Data, 0600); err != nil {
			return "", err
		}
		child, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if validationHome == "" {
			validationHome = stage
		}
		// Do not include core output: a rejected config may contain credentials.
		if err = exec.CommandContext(child, binary, "-t", "-d", validationHome, "-f", path).Run(); err != nil {
			return "", fmt.Errorf("Mihomo rejected the generated artifact: %w", err)
		}
		return "Mihomo core-check; application import untested", nil
	default:
		return "encoder-only; application import untested", nil
	}
}
