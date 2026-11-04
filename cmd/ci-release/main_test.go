package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSelectVersion(t *testing.T) {
	tags := []string{"v1.2.0", "v1.9.9", "v1.10.2", "v9.0.0-rc.1", "v01.99.0", "nightly"}
	for _, tc := range []struct {
		name    string
		base    string
		tags    []string
		request releaseRequest
		want    string
	}{
		{"ordinary CI", "v1.2.0", tags, releaseRequest{}, "v1.2.0"},
		{"tag wins over manifest", "v1.2.0", tags, releaseRequest{Tag: "v2.0.0", Publish: true}, "v2.0.0"},
		{"manual preview increments", "v1.2.0", tags, releaseRequest{Manual: true}, "v1.10.3"},
		{"publish patch", "v1.2.0", tags, releaseRequest{Manual: true, Publish: true, Bump: "patch"}, "v1.10.3"},
		{"publish minor", "v1.2.0", tags, releaseRequest{Manual: true, Publish: true, Bump: "minor"}, "v1.11.0"},
		{"publish major", "v1.2.0", tags, releaseRequest{Manual: true, Publish: true, Bump: "major"}, "v2.0.0"},
		{"initial version", "v0.0.0", nil, releaseRequest{Manual: true, Publish: true}, "v0.0.1"},
		{"manifest fallback", "v1.2.0", nil, releaseRequest{Manual: true, Publish: true}, "v1.2.1"},
		{"newer manifest", "v2.1.0", tags, releaseRequest{Manual: true, Publish: true}, "v2.1.1"},
		{"custom version", "v1.2.0", tags, releaseRequest{Manual: true, Publish: true, Version: "v3.4.5"}, "v3.4.5"},
		{"normalize custom", "v1.2.0", tags, releaseRequest{Manual: true, Publish: true, Version: " 3.4.5 "}, "v3.4.5"},
		{"preview existing version", "v1.2.0", tags, releaseRequest{Manual: true, Version: "v1.10.2"}, "v1.10.2"},
		{"ignore manual input on push", "v1.2.0", tags, releaseRequest{Version: "v8.0.0"}, "v1.2.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectVersion(tc.base, tc.tags, tc.request)
			if err != nil || got != tc.want {
				t.Fatalf("version = %q, error = %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestRejectInvalidReleaseRequest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		base    string
		request releaseRequest
	}{
		{"existing tag", "v1.2.0", releaseRequest{Manual: true, Publish: true, Version: "1.2.0"}},
		{"invalid tag", "v1.2.0", releaseRequest{Tag: "v2.0.0-rc.1", Publish: true}},
		{"invalid custom", "v1.2.0", releaseRequest{Manual: true, Version: "v2.0"}},
		{"leading zero", "v1.2.0", releaseRequest{Manual: true, Version: "v2.01.0"}},
		{"multiline input", "v1.2.0", releaseRequest{Manual: true, Version: "v2.0.0\nmalicious=value"}},
		{"shell input", "v1.2.0", releaseRequest{Manual: true, Version: "$(id)"}},
		{"invalid bump", "v1.2.0", releaseRequest{Manual: true, Bump: "prerelease"}},
		{"invalid manifest", "", releaseRequest{Manual: true}},
		{"component too large", "v1.2.0", releaseRequest{Manual: true, Version: "v18446744073709551616.0.0"}},
		{"patch overflow", "v1.2.18446744073709551615", releaseRequest{Manual: true}},
		{"minor overflow", "v1.18446744073709551615.0", releaseRequest{Manual: true, Bump: "minor"}},
		{"major overflow", "v18446744073709551615.0.0", releaseRequest{Manual: true, Bump: "major"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := selectVersion(tc.base, []string{"v1.2.0"}, tc.request); err == nil {
				t.Fatal("accepted invalid release request")
			}
		})
	}
}

func TestWorkflowReleaseGates(t *testing.T) {
	for _, tc := range []struct {
		name, event, ref, publish string
		manual, wantPublish       bool
		tag                       string
	}{
		{"branch push", "push", "refs/heads/main", "true", false, false, ""},
		{"pull request", "pull_request", "refs/pull/1/merge", "true", false, false, ""},
		{"nonversion tag", "push", "refs/tags/nightly", "true", false, false, ""},
		{"version tag", "push", "refs/tags/v2.3.4", "false", false, true, "v2.3.4"},
		{"manual default", "workflow_dispatch", "refs/heads/main", "", true, false, ""},
		{"manual preview", "workflow_dispatch", "refs/heads/main", "false", true, false, ""},
		{"manual publish", "workflow_dispatch", "refs/heads/main", "true", true, true, ""},
		{"manual on tag", "workflow_dispatch", "refs/tags/v2.3.4", "false", true, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"GITHUB_EVENT_NAME": tc.event, "GITHUB_REF": tc.ref, "RELEASE_PUBLISH": tc.publish}
			got, err := requestFromEnvironment(func(key string) string { return env[key] })
			if err != nil || got.Manual != tc.manual || got.Publish != tc.wantPublish || got.Tag != tc.tag {
				t.Fatalf("request = %+v, error = %v", got, err)
			}
		})
	}
	env := map[string]string{"GITHUB_EVENT_NAME": "workflow_dispatch", "RELEASE_PUBLISH": "maybe"}
	if _, err := requestFromEnvironment(func(key string) string { return env[key] }); err == nil {
		t.Fatal("accepted invalid publish flag")
	}
}

func releaseFixture(t *testing.T) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	data := []byte("{\n  \"release\": \"v1.2.0\",\n  \"go\": \"1.27.3\",\n  \"sing_box\": {\"tested_version\": \"1.14.4\"},\n  \"extra\": [1, true]\n}\n")
	if err := os.WriteFile(filepath.Join(root, "versions.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"add", "versions.json"},
		{"-c", "user.name=CI Test", "-c", "user.email=ci@example.invalid", "-c", "core.hooksPath=" + filepath.Join(root, "no-hooks"), "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture"},
		{"-c", "tag.gpgSign=false", "tag", "v1.2.0"},
		{"-c", "tag.gpgSign=false", "tag", "v1.3.2"},
	} {
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	return root, data
}

func TestPrepareReleaseSynchronizesManifest(t *testing.T) {
	root, original := releaseFixture(t)
	got, err := prepareRelease(root, releaseRequest{Manual: true, Publish: true})
	if err != nil || got != "v1.3.3" {
		t.Fatalf("version = %q, error = %v", got, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "versions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	if err = json.Unmarshal(original, &before); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	before["release"] = got
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("manifest fields changed unexpectedly: %s", data)
	}
	command := exec.Command("git", "tag", "--list", got)
	command.Dir = root
	output, err := command.Output()
	if err != nil || len(output) != 0 {
		t.Fatalf("preparation must not create a tag: %s, %v", output, err)
	}
}

func TestPrepareReleaseLeavesManifestUntouched(t *testing.T) {
	for _, request := range []releaseRequest{
		{},
		{Manual: true, Publish: true, Version: "v1.2.0"},
		{Tag: "v2.0.0-beta.1", Publish: true},
	} {
		root, original := releaseFixture(t)
		_, err := prepareRelease(root, request)
		if (request.Publish || request.Tag != "") && err == nil {
			t.Fatal("expected invalid request to fail")
		}
		if !request.Publish && request.Tag == "" && err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(root, "versions.json"))
		if err != nil || string(data) != string(original) {
			t.Fatalf("manifest changed: %s, %v", data, err)
		}
	}
}

func TestAppendGitHubOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(path, []byte("previous=value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := appendText(path, "version=v1.2.3\n"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(data)) != "previous=value\nversion=v1.2.3" {
		t.Fatalf("unexpected workflow output: %s, %v", data, err)
	}
}
