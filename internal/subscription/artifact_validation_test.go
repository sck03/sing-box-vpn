package subscription

import "testing"

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
