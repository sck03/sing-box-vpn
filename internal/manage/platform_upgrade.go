package manage

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/command"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var programVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

func (s *PlatformService) upgradeProgram(ctx context.Context, version string) error {
	if s.NoSystem {
		return errors.New("program deployment requires Linux systemd")
	}
	endpoint := "https://api.github.com/repos/sck03/sing-box-vpn/releases/latest"
	if version != "" {
		if !programVersion.MatchString(version) {
			return errors.New("invalid release tag")
		}
		endpoint = "https://api.github.com/repos/sck03/sing-box-vpn/releases/tags/" + version
	}
	client := newHTTPClient(3 * time.Minute)
	defer client.CloseIdleConnections()
	var release coreRelease
	if err := getJSON(ctx, client, endpoint, &release); err != nil {
		return err
	}
	if !programVersion.MatchString(release.Tag) {
		return errors.New("release tag is invalid")
	}
	name := "sing-box-vpn-" + release.Tag + ".tar.gz"
	var chosen releaseAsset
	for _, asset := range release.Assets {
		if asset.Name == name {
			chosen = asset
		}
	}
	if chosen.URL != "https://github.com/sck03/sing-box-vpn/releases/download/"+release.Tag+"/"+name || !digestPattern.MatchString(chosen.Digest) {
		return errors.New("release has no verified compatible bundle")
	}
	stage, err := os.MkdirTemp(s.Root, ".program-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	archive := filepath.Join(stage, "release.tar.gz")
	if err = downloadAsset(ctx, client, chosen, archive); err != nil {
		return err
	}
	if err = extractProgramArchive(archive, stage); err != nil {
		return err
	}
	// The separate unit survives restarting this panel. The updater finishes all
	// core checks first and defers only the panel restart until its caller exits.
	_, err = command.Output(ctx, nil, "systemd-run", "--unit=sb-program-update", "--wait", "--collect", "--property=Type=exec", "--setenv=SB_ROOT="+s.Root, "--setenv=SB_DEFER_PANEL_RESTART=1", "/bin/bash", filepath.Join(stage, "sing-box-vpn", "sb.sh"), "upgrade")
	return err
}

func extractProgramArchive(path, root string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	var total int64
	for count := 0; ; count++ {
		header, e := reader.Next()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return e
		}
		if count >= 10000 {
			return errors.New("too many release entries")
		}
		name := strings.TrimSuffix(header.Name, "/")
		if !strings.HasPrefix(name, "sing-box-vpn/") && name != "sing-box-vpn" {
			return errors.New("unexpected release root")
		}
		if !filepath.IsLocal(name) || strings.Contains(name, "\\") || filepath.ToSlash(filepath.Clean(name)) != name {
			return errors.New("invalid release path")
		}
		if header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg {
			return errors.New("release links and special files are prohibited")
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		if header.Typeflag == tar.TypeDir {
			if e = os.MkdirAll(target, 0755); e != nil {
				return e
			}
			continue
		}
		total += header.Size
		if header.Size < 0 || total > 512<<20 {
			return errors.New("release exceeds extraction budget")
		}
		if e = os.MkdirAll(filepath.Dir(target), 0755); e != nil {
			return e
		}
		mode := os.FileMode(0644)
		if header.Mode&0111 != 0 {
			mode = 0755
		}
		out, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if e != nil {
			return e
		}
		_, e = io.CopyN(out, reader, header.Size)
		e = errors.Join(e, out.Close())
		if e != nil {
			return e
		}
	}
	return nil
}

func (s *PlatformService) rollbackRuntime(ctx context.Context) error {
	if s.NoSystem {
		return errors.New("runtime rollback requires Linux systemd")
	}
	previous := s.Runtime + ".previous"
	if _, err := os.Stat(previous); err != nil {
		return errors.New("no previous managed runtime is available")
	}
	state, err := s.Store.State(ctx)
	if err != nil {
		return err
	}
	c, err := decodePlatform(state.Applied)
	if err != nil {
		return err
	}
	files, err := s.build(ctx, c)
	if err != nil {
		return err
	}
	probe := PlatformService{Root: s.Root, Runtime: previous}
	if err = probe.checkRuntime(ctx, files); err != nil {
		return fmt.Errorf("previous runtime is incompatible: %w", err)
	}
	active, err := s.activeServices(ctx)
	if err != nil {
		return err
	}
	backup := s.Runtime + ".rollback-current"
	if err = copyBinary(s.Runtime, backup); err != nil {
		return err
	}
	if err = copyBinary(previous, s.Runtime+".candidate"); err != nil {
		return err
	}
	if err = os.Rename(s.Runtime+".candidate", s.Runtime); err != nil {
		return err
	}
	for _, name := range active {
		if name == "sing-box" || name == "vpngate-core" {
			started := time.Now().UnixNano()
			if err = s.service(ctx, name, "restart"); err != nil {
				break
			}
			scope := "normal"
			if name == "vpngate-core" {
				scope = "vpngate"
			}
			if err = s.awaitRuntime(ctx, scope, started); err != nil {
				break
			}
		}
	}
	if err != nil {
		restore, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		e := copyBinary(backup, s.Runtime+".candidate")
		if e == nil {
			e = os.Rename(s.Runtime+".candidate", s.Runtime)
		}
		for _, name := range active {
			if name == "sing-box" || name == "vpngate-core" {
				e = errors.Join(e, s.service(restore, name, "restart"))
			}
		}
		return errors.Join(err, e)
	}
	return os.Rename(backup, previous)
}
