package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var stableVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type version [3]uint64

func parseVersion(value string) (version, error) {
	var result version
	parts := stableVersion.FindStringSubmatch(value)
	if parts == nil {
		return result, fmt.Errorf("invalid stable release version %q: expected vMAJOR.MINOR.PATCH", value)
	}
	for i := range result {
		n, err := strconv.ParseUint(parts[i+1], 10, 64)
		if err != nil {
			return result, fmt.Errorf("invalid version component: %w", err)
		}
		result[i] = n
	}
	return result, nil
}

func (v version) String() string { return fmt.Sprintf("v%d.%d.%d", v[0], v[1], v[2]) }

func (v version) after(other version) bool {
	for i := range v {
		if v[i] != other[i] {
			return v[i] > other[i]
		}
	}
	return false
}

func (v version) bump(part string) (version, error) {
	index := 2
	switch part {
	case "", "patch":
	case "minor":
		index = 1
	case "major":
		index = 0
	default:
		return v, fmt.Errorf("invalid version bump %q: expected patch, minor or major", part)
	}
	if v[index] == ^uint64(0) {
		return v, errors.New("version component overflow")
	}
	v[index]++
	for i := index + 1; i < len(v); i++ {
		v[i] = 0
	}
	return v, nil
}

type releaseRequest struct {
	Manual  bool
	Publish bool
	Tag     string
	Version string
	Bump    string
}

func selectVersion(base string, tags []string, request releaseRequest) (string, error) {
	current, err := parseVersion(base)
	if err != nil {
		return "", fmt.Errorf("versions.json: %w", err)
	}
	if request.Tag != "" {
		selected, err := parseVersion(request.Tag)
		return selected.String(), err
	}
	if !request.Manual {
		return current.String(), nil
	}
	if requested := strings.TrimSpace(request.Version); requested != "" {
		if !strings.HasPrefix(requested, "v") {
			requested = "v" + requested
		}
		selected, err := parseVersion(requested)
		if err != nil {
			return "", err
		}
		if request.Publish {
			for _, tag := range tags {
				if tag == selected.String() {
					return "", fmt.Errorf("tag %s already exists; choose a new version or rerun its original tag workflow", tag)
				}
			}
		}
		return selected.String(), nil
	}
	for _, tag := range tags {
		candidate, err := parseVersion(tag)
		if err == nil && candidate.after(current) {
			current = candidate
		}
	}
	next, err := current.bump(request.Bump)
	return next.String(), err
}

func prepareRelease(root string, request releaseRequest) (string, error) {
	path := filepath.Join(root, "versions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var manifest map[string]json.RawMessage
	if err = json.Unmarshal(data, &manifest); err != nil {
		return "", err
	}
	var base string
	if err = json.Unmarshal(manifest["release"], &base); err != nil {
		return "", fmt.Errorf("read release from versions.json: %w", err)
	}
	command := exec.Command("git", "tag", "--list", "v*")
	command.Dir = root
	tagOutput, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("list version tags: %w", err)
	}
	selected, err := selectVersion(base, strings.Fields(string(tagOutput)), request)
	if err != nil {
		return "", err
	}
	if selected != base {
		manifest["release"], err = json.Marshal(selected)
		if err != nil {
			return "", err
		}
		data, err = json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return "", err
		}
		if err = os.WriteFile(path, append(data, '\n'), 0644); err != nil {
			return "", err
		}
	}
	return selected, nil
}

func appendText(path, text string) error {
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, err = file.WriteString(text)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func requestFromEnvironment(getenv func(string) string) (releaseRequest, error) {
	request := releaseRequest{
		Manual:  getenv("GITHUB_EVENT_NAME") == "workflow_dispatch",
		Version: getenv("RELEASE_VERSION"),
		Bump:    getenv("RELEASE_BUMP"),
	}
	if request.Manual {
		value := getenv("RELEASE_PUBLISH")
		if value != "" {
			var err error
			request.Publish, err = strconv.ParseBool(value)
			if err != nil {
				return request, fmt.Errorf("invalid publish option: %w", err)
			}
		}
	} else if ref := getenv("GITHUB_REF"); getenv("GITHUB_EVENT_NAME") == "push" && strings.HasPrefix(ref, "refs/tags/v") {
		request.Tag = strings.TrimPrefix(ref, "refs/tags/")
		request.Publish = true
	}
	return request, nil
}

func run() error {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	request, err := requestFromEnvironment(os.Getenv)
	if err != nil {
		return err
	}
	selected, err := prepareRelease(*root, request)
	if err != nil {
		return err
	}
	if err = appendText(os.Getenv("GITHUB_OUTPUT"), "version="+selected+"\n"); err != nil {
		return err
	}
	status := "Verification only; no tag or GitHub Release will be created."
	if request.Publish {
		status = "Publish after all verification steps pass."
	}
	if err = appendText(os.Getenv("GITHUB_STEP_SUMMARY"), "Release version: **"+selected+"**\n\n"+status+"\n"); err != nil {
		return err
	}
	fmt.Println("Release version:", selected)
	fmt.Println(status)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
