package manage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestClientExportsDoNotNeedMihomo(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MIHOMO_BINARY", filepath.Join(root, "must-not-exist"))
	t.Setenv("SBVPN_REQUIRE_CLIENT_CHECK", "1")
	node := sub.Node{Name: "test", Protocol: "trojan", Server: "example.com", Port: 443, Password: "secret", TLS: sub.TLS{Enabled: true}}
	for _, client := range []string{"mihomo", "openclash"} {
		for _, mode := range []string{"provider", "config"} {
			t.Run(client+"/"+mode, func(t *testing.T) {
				template := sub.Template{ID: "client", Client: client, Version: 1, Mode: mode}
				artifact, _, err := sub.Render([]sub.Node{node}, template, false)
				if err != nil {
					t.Fatal(err)
				}
				result, err := validateSubscriptionArtifact(t.Context(), root, "", template, artifact)
				if err != nil || result.State != "passed" || result.Scope != "parser" || result.ApplicationVerified {
					t.Fatal(result, err)
				}
				result, err = validateSubscriptionArtifact(t.Context(), root, "", template, sub.Artifact{Data: []byte("{invalid")})
				if err == nil || result.State != "failed" {
					t.Fatal("invalid output passed", result, err)
				}
			})
		}
	}
	if err := (application.TaskCommand{Operation: "validator-install"}).Validate(); err == nil {
		t.Fatal("removed installer task accepted")
	}
	service := PlatformService{Root: root, Core: filepath.Join(root, "sing-box"), Runtime: filepath.Join(root, "managed-core")}
	status, err := service.ProgramStatus(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, component := range status.Components {
		if component.ID == "mihomo" {
			t.Fatal("client exposed as server component")
		}
	}
}

func TestUnsupportedProbeIsNotNodeFailure(t *testing.T) {
	node := sub.Node{Name: "xhttp", Protocol: "vless", Server: "example.com", Port: 443, UUID: "00000000-0000-4000-8000-000000000001", TLS: sub.TLS{Enabled: true}, Transport: sub.Transport{Type: "xhttp", Path: "/"}}
	root := t.TempDir()
	result := checkSubscriptionNode(t.Context(), root, filepath.Join(root, "must-not-start"), "https://example.com", node)
	if result.Success || result.State != "unsupported" {
		t.Fatal(result)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result = checkSubscriptionNode(ctx, root, "", "https://example.com", node)
	if result.State != "cancelled" {
		t.Fatal(result)
	}
}

func TestProbeSummaryDistinguishesUnsupportedAndFailed(t *testing.T) {
	for _, test := range []struct {
		states          []string
		passed, skipped int
		outcome         string
	}{
		{[]string{"passed"}, 1, 0, "passed"},
		{[]string{"unsupported"}, 0, 1, "not_run"},
		{[]string{"passed", "unsupported"}, 1, 1, "partial"},
		{[]string{"failed", "unsupported"}, 0, 1, "partial"},
		{[]string{"failed"}, 0, 0, "failed"},
		{[]string{"passed", "cancelled"}, 1, 0, "partial"},
	} {
		var checks []subscriptionCheck
		for _, state := range test.states {
			checks = append(checks, subscriptionCheck{State: state, Success: state == "passed"})
		}
		passed, skipped, outcome := summarizeSubscriptionChecks(checks)
		if passed != test.passed || skipped != test.skipped || outcome != test.outcome {
			t.Fatal(test, passed, skipped, outcome)
		}
	}
}
