package manage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxAssetSize = 256 << 20

func RunAsset(ctx context.Context, args []string, output io.Writer) error {
	f := flag.NewFlagSet("asset", flag.ContinueOnError)
	f.SetOutput(output)
	arch := f.String("arch", "", "amd64 or arm64")
	destination := f.String("output", "", "cloudflared path")
	if err := f.Parse(args); err != nil {
		return err
	}
	if (*arch != "amd64" && *arch != "arm64") || *destination == "" {
		return errors.New("需要 --arch amd64|arm64 和 --output")
	}
	client := newHTTPClient(90 * time.Second)
	defer client.CloseIdleConnections()
	var release coreRelease
	if err := getJSON(ctx, client, "https://api.github.com/repos/cloudflare/cloudflared/releases/latest", &release); err != nil {
		return err
	}
	name := "cloudflared-linux-" + *arch
	for _, asset := range release.Assets {
		if asset.Name != name {
			continue
		}
		expected := "https://github.com/cloudflare/cloudflared/releases/download/" + release.Tag + "/" + name
		if asset.URL != expected || !digestPattern.MatchString(asset.Digest) {
			return errors.New("发行资产缺少可验证的官方地址或 SHA256")
		}
		return downloadAsset(ctx, client, asset, *destination)
	}
	return errors.New("发行缺少本机架构的 cloudflared")
}

func downloadAsset(ctx context.Context, client *http.Client, asset releaseAsset, destination string) error {
	return atomicWrite(destination, 0755, func(output io.Writer) error {
		return copyVerifiedAsset(ctx, client, asset, output)
	})
}

// Core archives and standalone assets share download limits and verification.
func copyVerifiedAsset(ctx context.Context, client *http.Client, asset releaseAsset, output io.Writer) error {
	if !digestPattern.MatchString(asset.Digest) {
		return errors.New("发行资产缺少 SHA256")
	}
	response, err := httpRequest(ctx, client, asset.URL)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.ContentLength > maxAssetSize {
		return errors.New("资源超过 256 MiB")
	}
	digest := sha256.New()
	size, err := copyFileContent(io.MultiWriter(output, digest), io.LimitReader(response.Body, maxAssetSize+1))
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if size > maxAssetSize {
		return errors.New("资源超过 256 MiB")
	}
	if hex.EncodeToString(digest.Sum(nil)) != strings.ToLower(asset.Digest[7:]) {
		return errors.New("SHA256 不匹配")
	}
	return nil
}
