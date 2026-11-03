package manage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
		if err := os.MkdirAll(filepath.Dir(*destination), 0700); err != nil {
			return err
		}
		file, err := os.CreateTemp(filepath.Dir(*destination), ".asset-")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		response, err := httpRequest(ctx, client, asset.URL)
		if err != nil {
			file.Close()
			return err
		}
		digest := sha256.New()
		size, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(response.Body, (256<<20)+1))
		response.Body.Close()
		if err == nil {
			err = file.Sync()
		}
		if err == nil {
			err = file.Chmod(0755)
		}
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if size > 256<<20 {
			return errors.New("资源超过大小限制")
		}
		if hex.EncodeToString(digest.Sum(nil)) != strings.ToLower(asset.Digest[7:]) {
			return errors.New("SHA256 不匹配")
		}
		return os.Rename(file.Name(), *destination)
	}
	return errors.New("发行缺少本机架构的 cloudflared")
}
