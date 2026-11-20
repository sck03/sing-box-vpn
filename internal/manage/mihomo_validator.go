package manage

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	project "github.com/sck03/sing-box-vpn"
)

var mihomoVersion = regexp.MustCompile(`(?m)^Mihomo (?:Meta )?v?([0-9]+\.[0-9]+\.[0-9]+)(?:\s|$)`)

func checkMihomo(ctx context.Context, binary string) (string, error) {
	output, err := commandOutput(ctx, binary, "-v")
	if err != nil {
		return "", err
	}
	match := mihomoVersion.FindStringSubmatch(string(output))
	if len(match) != 2 {
		return "", errors.New("invalid Mihomo stable version")
	}
	return match[1], nil
}

func mihomoAsset(release coreRelease, arch string) (releaseAsset, error) {
	version := project.Versions.Mihomo.Tested
	if release.Draft || release.Prerelease || release.Tag != "v"+version || (arch != "amd64" && arch != "arm64") {
		return releaseAsset{}, errors.New("unsupported Mihomo release")
	}
	assetArch, digest := arch, project.Versions.Mihomo.ARM64
	if arch == "amd64" {
		// The unsuffixed upstream amd64 binary requires x86-64-v3. The
		// validator must also run on baseline CPUs and virtual machines.
		assetArch, digest = "amd64-v1", project.Versions.Mihomo.AMD64
	}
	name := "mihomo-linux-" + assetArch + "-v" + version + ".gz"
	expected := "https://github.com/MetaCubeX/mihomo/releases/download/v" + version + "/" + name
	for _, asset := range release.Assets {
		if asset.Name != name {
			continue
		}
		if asset.URL != expected {
			return releaseAsset{}, errors.New("unexpected Mihomo asset URL")
		}
		expectedDigest := "sha256:" + digest
		if asset.Digest != "" && asset.Digest != expectedDigest {
			return releaseAsset{}, errors.New("Mihomo asset digest differs from release manifest")
		}
		asset.Digest = expectedDigest
		if !digestPattern.MatchString(asset.Digest) {
			return releaseAsset{}, errors.New("Mihomo asset lacks a verified SHA256")
		}
		return asset, nil
	}
	return releaseAsset{}, errors.New("Mihomo binary unavailable for this architecture")
}

func (s *PlatformService) installMihomo(ctx context.Context) error {
	if s.NoSystem || runtime.GOOS != "linux" {
		return errors.New("validator installation requires Linux")
	}
	client := newHTTPClient(3 * time.Minute)
	defer client.CloseIdleConnections()
	var release coreRelease
	if err := getJSON(ctx, client, "https://api.github.com/repos/MetaCubeX/mihomo/releases/tags/v"+project.Versions.Mihomo.Tested, &release); err != nil {
		return err
	}
	asset, err := mihomoAsset(release, runtime.GOARCH)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(s.Root, ".mihomo-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	archive := filepath.Join(stage, "validator.gz")
	if err = downloadAsset(ctx, client, asset, archive); err != nil {
		return err
	}
	input, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer input.Close()
	compressed, err := gzip.NewReader(input)
	if err != nil {
		return err
	}
	defer compressed.Close()
	candidate := filepath.Join(stage, "mihomo")
	err = writeCoreStage(candidate, 0755, func(output io.Writer) error {
		n, e := io.Copy(output, io.LimitReader(compressed, (256<<20)+1))
		if n > 256<<20 {
			return errors.New("Mihomo binary exceeds limit")
		}
		return e
	})
	if err != nil {
		return err
	}
	version, err := checkMihomo(ctx, candidate)
	if err != nil {
		return err
	}
	if version != strings.TrimPrefix(release.Tag, "v") {
		return errors.New("Mihomo binary version mismatch")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(candidate, filepath.Join(s.Root, "mihomo"))
}
