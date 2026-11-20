package deployment

import (
	"strings"
	"testing"
)

func TestBundleRejectsUnverifiedOrAmbiguousReleases(t *testing.T) {
	release := Release{Tag: "v5.1.0", Assets: []Asset{{Name: "sing-box-vpn-v5.1.0.tar.gz", URL: "https://github.com/sck03/sing-box-vpn/releases/download/v5.1.0/sing-box-vpn-v5.1.0.tar.gz", Digest: "sha256:" + strings.Repeat("a", 64)}}}
	if _, err := Bundle(release); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Release){func(r *Release) { r.Draft = true }, func(r *Release) { r.Prerelease = true }, func(r *Release) { r.Assets[0].URL = "https://example.test/bundle" }, func(r *Release) { r.Assets[0].Digest = "" }, func(r *Release) { r.Assets = append(r.Assets, r.Assets[0]) }} {
		copy := release
		copy.Assets = append([]Asset(nil), release.Assets...)
		mutate(&copy)
		if _, err := Bundle(copy); err == nil {
			t.Fatal("invalid release accepted", copy)
		}
	}
}
func TestVersionsCompareNumerically(t *testing.T) {
	if !Newer("v5.10.0", "v5.9.9") || Newer("v5.1.0", "v5.1.0") || Newer("v5.0.3", "v5.1.0") || Newer("v5.2.0-beta", "v5.1.0") {
		t.Fatal("incorrect version ordering")
	}
	if _, err := Endpoint("v5.1.0/other"); err == nil {
		t.Fatal("invalid tag accepted")
	}
}
