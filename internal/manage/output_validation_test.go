package manage

import (
	project "github.com/sck03/sing-box-vpn"
	"strings"
	"testing"
)

func TestMihomoInstallerRequiresExactVerifiedAsset(t *testing.T) {
	v := project.Versions.Mihomo.Tested
	name := "mihomo-linux-amd64-v1-v" + v + ".gz"
	good := coreRelease{Tag: "v" + v, Assets: []releaseAsset{{Name: name, URL: "https://github.com/MetaCubeX/mihomo/releases/download/v" + v + "/" + name}}}
	asset, err := mihomoAsset(good, "amd64")
	if err != nil || asset.Digest != "sha256:"+project.Versions.Mihomo.AMD64 {
		t.Fatal(asset, err)
	}
	armName := "mihomo-linux-arm64-v" + v + ".gz"
	arm := coreRelease{Tag: "v" + v, Assets: []releaseAsset{{Name: armName, URL: "https://github.com/MetaCubeX/mihomo/releases/download/v" + v + "/" + armName}}}
	asset, err = mihomoAsset(arm, "arm64")
	if err != nil || asset.Digest != "sha256:"+project.Versions.Mihomo.ARM64 {
		t.Fatal(asset, err)
	}
	for _, kind := range []string{"host", "digest", "digest-mismatch", "version", "prerelease", "arch", "v3"} {
		t.Run(kind, func(t *testing.T) {
			r := good
			r.Assets = append([]releaseAsset{}, good.Assets...)
			arch := "amd64"
			switch kind {
			case "host":
				r.Assets[0].URL = strings.Replace(r.Assets[0].URL, "github.com", "example.test", 1)
			case "digest":
				r.Assets[0].Digest = "sha256:bad"
			case "digest-mismatch":
				r.Assets[0].Digest = "sha256:" + strings.Repeat("0", 64)
			case "v3":
				r.Assets[0].Name = "mihomo-linux-amd64-v" + v + ".gz"
			case "version":
				r.Tag = "v0.0.0"
			case "prerelease":
				r.Prerelease = true
			case "arch":
				arch = "mips"
			}
			if _, err := mihomoAsset(r, arch); err == nil {
				t.Fatal("unverified asset accepted")
			}
		})
	}
}

func TestHeavyRequestsLeaveStatusCapacity(t *testing.T) {
	for _, path := range []string{"/api/config", "/api/config/preview", "/api/config/preview-output", "/api/accounts/export", "/api/grants", "/api/restore"} {
		if !expensivePanelRequest(path, "POST") {
			t.Fatal(path)
		}
	}
	for _, path := range []string{"/api/config", "/api/overview", "/api/accounts/outputs", "/api/events", "/healthz"} {
		if expensivePanelRequest(path, "GET") {
			t.Fatal(path)
		}
	}
}
