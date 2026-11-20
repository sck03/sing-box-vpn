// Package deployment owns release identity and compatible bundle selection.
package deployment

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var TagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}
type Release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

const Repository = "sck03/sing-box-vpn"

func Endpoint(version string) (string, error) {
	base := "https://api.github.com/repos/" + Repository + "/releases/"
	if version == "" {
		return base + "latest", nil
	}
	if !TagPattern.MatchString(version) {
		return "", errors.New("invalid release tag")
	}
	return base + "tags/" + version, nil
}
func Bundle(release Release) (Asset, error) {
	if !TagPattern.MatchString(release.Tag) || release.Draft || release.Prerelease {
		return Asset{}, errors.New("only formal stable project releases are supported")
	}
	name := "sing-box-vpn-" + release.Tag + ".tar.gz"
	var selected Asset
	for _, asset := range release.Assets {
		if asset.Name != name {
			continue
		}
		if selected.Name != "" {
			return Asset{}, errors.New("ambiguous release bundle")
		}
		selected = asset
	}
	if selected.URL != "https://github.com/"+Repository+"/releases/download/"+release.Tag+"/"+name || !digestPattern.MatchString(selected.Digest) {
		return Asset{}, errors.New("release has no verified compatible bundle")
	}
	return selected, nil
}
func Newer(candidate, current string) bool {
	if !TagPattern.MatchString(candidate) || !TagPattern.MatchString(current) {
		return false
	}
	a, b := strings.Split(strings.TrimPrefix(candidate, "v"), "."), strings.Split(strings.TrimPrefix(current, "v"), ".")
	if len(a) != 3 || len(b) != 3 {
		return false
	}
	for i := range a {
		x, e := strconv.ParseUint(a[i], 10, 32)
		y, f := strconv.ParseUint(b[i], 10, 32)
		if e != nil || f != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return false
}
func ReleaseURL(tag string) string {
	return fmt.Sprintf("https://github.com/%s/releases/tag/%s", Repository, tag)
}
