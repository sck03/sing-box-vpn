package subscription

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestArtifactParsersCoverGeneratedClientModes(t *testing.T) {
	for client := range artifactParsers {
		for _, mode := range Capabilities()[client].Modes {
			if mode == "rules" {
				continue
			}
			t.Run(client+"/"+mode, func(t *testing.T) {
				template := Template{ID: "verify", Client: client, Mode: mode, Version: 1}
				artifact, _, err := Render(testNodes(), template, true)
				if err != nil {
					t.Fatal(err)
				}
				result, err := ValidateArtifact(template, artifact)
				if err != nil || result.Scope != "parser" || result.State != "passed" || result.ApplicationVerified {
					t.Fatal(result, err)
				}
			})
		}
	}
}

func TestTextCommentNamesNeverSilentlyDisappear(t *testing.T) {
	for _, client := range []string{"surge", "surge-mac", "surfboard", "loon", "quantumult-x"} {
		for _, name := range []string{"#hidden", ";hidden", "//hidden", "  #hidden"} {
			nodes := []Node{{Name: name, Protocol: "trojan", Server: "example.com", Port: 443, Password: "fixture", TLS: TLS{Enabled: true}}, {Name: "visible", Protocol: "trojan", Server: "example.com", Port: 8443, Password: "fixture", TLS: TLS{Enabled: true}}}
			template := Template{ID: "test", Version: 1, Client: client, Mode: "nodes"}
			if _, _, err := Render(nodes, template, false); err == nil {
				t.Fatalf("%s accepted comment name %q", client, name)
			}
			artifact, diagnostics, err := Render(nodes, template, true)
			if err != nil || artifact.NodeCount != 1 || len(diagnostics) != 1 {
				t.Fatal(client, artifact.NodeCount, diagnostics, err)
			}
			if _, err := ValidateArtifact(template, artifact); err != nil {
				t.Fatal(client, err)
			}
		}
	}
}

func TestArtifactValidationRejectsLostOrChangedConnections(t *testing.T) {
	nodes := []Node{{Name: "one", Protocol: "trojan", Server: "example.com", Port: 443, Password: "fixture-secret", TLS: TLS{Enabled: true}}, {Name: "two", Protocol: "trojan", Server: "example.com", Port: 8443, Password: "fixture-secret", TLS: TLS{Enabled: true}}}
	template := Template{ID: "test", Version: 1, Client: "surge", Mode: "nodes"}
	artifact, _, err := Render(nodes, template, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { return append([]byte("#"), b...) },
		func(b []byte) []byte { return bytes.ReplaceAll(b, []byte("fixture-secret"), []byte("wrong-secret")) },
		func(b []byte) []byte { return bytes.ReplaceAll(b, []byte("example.com"), []byte("other.example.com")) },
	} {
		changed := artifact
		changed.Data = mutate(artifact.Data)
		result, err := ValidateArtifact(template, changed)
		if err == nil || result.State != "failed" {
			t.Fatal("accepted semantic corruption", result)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("error exposed credentials")
		}
	}
}

func TestStashOwnsDNSAndPreservesNodes(t *testing.T) {
	template := Template{ID: "stash", Version: 1, Client: "stash", Mode: "config", Preset: "split"}
	artifact, _, err := Render(testNodes(), template, true)
	if err != nil {
		t.Fatal(err)
	}
	var config Object
	if err := json.Unmarshal(artifact.Data, &config); err != nil {
		t.Fatal(err)
	}
	dns := obj(config["dns"])
	if dns["follow-rule"] != true {
		t.Fatal("DNS routing missing")
	}
	for _, key := range []string{"enhanced-mode", "respect-rules", "enable"} {
		if _, ok := dns[key]; ok {
			t.Fatal("Mihomo DNS field leaked", key)
		}
	}
	if _, err := ValidateArtifact(template, artifact); err != nil {
		t.Fatal(err)
	}
	off := false
	template.DNSPolicy = &DNSPolicy{FakeIP: &off}
	if _, _, err := Render(testNodes(), template, true); err == nil {
		t.Fatal("accepted unsupported fake-IP switch")
	}
}
