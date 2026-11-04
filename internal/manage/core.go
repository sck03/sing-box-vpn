package manage

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	project "github.com/sck03/sing-box-vpn"
)

var stablePattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
var coreVersionPattern = regexp.MustCompile(`sing-box version (\d+\.\d+\.\d+)([^\s]*)`)
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-fA-F]{64}$`)

func stableVersion(value string) (string, error) {
	value = strings.TrimPrefix(value, "v")
	if !stablePattern.MatchString(value) {
		return "", errors.New("请输入正式稳定版，例如 1.14.4")
	}
	parts, minimum := strings.Split(value, "."), strings.Split(project.Versions.SingBox.Minimum, ".")
	for i := range parts {
		n, err := strconv.ParseUint(parts[i], 10, 32)
		if err != nil {
			return "", errors.New("版本号超出范围")
		}
		m, _ := strconv.ParseUint(minimum[i], 10, 32)
		if n > m {
			return value, nil
		}
		if n < m {
			return "", errors.New("不支持低于 " + project.Versions.SingBox.Minimum + " 的旧核心")
		}
	}
	return value, nil
}

func commandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	child, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(child, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s: %w: %s", filepath.Base(name), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func CheckCore(ctx context.Context, binary string) (string, error) {
	output, err := commandOutput(ctx, binary, "version")
	if err != nil {
		return "", err
	}
	match := coreVersionPattern.FindStringSubmatch(string(output))
	if len(match) != 3 || strings.HasPrefix(match[2], "-") {
		return "", errors.New("需要 sing-box 正式稳定版")
	}
	return stableVersion(match[1])
}

func CheckJSON(ctx context.Context, binary, directory string, value any) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(directory, ".check-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	data, err := JSONBytes(value)
	if err == nil {
		_, err = f.Write(data)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	_, err = commandOutput(ctx, binary, "check", "-c", f.Name())
	if err != nil {
		return fmt.Errorf("配置检查失败: %w", err)
	}
	return nil
}

func withLock(root string, operation func() error) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(root, ".manage.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = lockFile(f); err != nil {
		return err
	}
	return operation()
}

func TryLockFile(file *os.File) error { return lockFile(file) }

type releaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}
type coreRelease struct {
	Tag        string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}
type verifiedAsset struct{ Version, Name, URL, SHA256, Arch string }

func selectAsset(release coreRelease, arch string) (verifiedAsset, error) {
	if release.Draft || release.Prerelease {
		return verifiedAsset{}, errors.New("该发行不是正式稳定版")
	}
	version, err := stableVersion(release.Tag)
	if err != nil {
		return verifiedAsset{}, err
	}
	name := "sing-box-" + version + "-linux-" + arch + ".tar.gz"
	expectedURL := "https://github.com/SagerNet/sing-box/releases/download/v" + version + "/" + name
	for _, asset := range release.Assets {
		if asset.Name != name {
			continue
		}
		if asset.URL != expectedURL {
			return verifiedAsset{}, errors.New("发行下载地址不是预期官方地址")
		}
		digest := asset.Digest
		if digest == "" && version == project.Versions.SingBox.Tested {
			if arch == "amd64" {
				digest = "sha256:" + project.Versions.SingBox.AMD64
			}
			if arch == "arm64" {
				digest = "sha256:" + project.Versions.SingBox.ARM64
			}
		}
		if !digestPattern.MatchString(digest) {
			return verifiedAsset{}, errors.New("官方发行未提供可验证 SHA256")
		}
		return verifiedAsset{version, name, expectedURL, strings.ToLower(digest[7:]), arch}, nil
	}
	return verifiedAsset{}, errors.New("官方发行缺少本机架构二进制")
}

func newHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns, transport.MaxIdleConnsPerHost = 8, 2
	transport.TLSHandshakeTimeout = 10 * time.Second
	return &http.Client{Timeout: timeout, Transport: transport}
}
func httpRequest(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sing-box-vpn/"+strings.TrimPrefix(project.Versions.Release, "v"))
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("远端请求失败，检查网络与服务状态")
	}
	if response.StatusCode != 200 {
		response.Body.Close()
		return nil, fmt.Errorf("HTTP response %d", response.StatusCode)
	}
	return response, nil
}
func getJSON(ctx context.Context, client *http.Client, url string, target any) error {
	response, err := httpRequest(ctx, client, url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return errors.New("HTTP response too large")
	}
	return json.Unmarshal(data, target)
}

type serviceManager interface {
	active(context.Context) ([]string, error)
	restart(context.Context, string) error
}
type systemServices struct{}

func (systemServices) active(ctx context.Context) ([]string, error) {
	var active []string
	for _, name := range []string{"sing-box", "sing-box-vpngate"} {
		child, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := exec.CommandContext(child, "systemctl", "is-active", "--quiet", name).Run()
		cancel()
		if err == nil {
			active = append(active, name)
		} else {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				return nil, err
			}
		}
	}
	return active, nil
}
func (systemServices) restart(ctx context.Context, name string) error {
	child, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(child, "systemctl", "restart", name).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, output)
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	_, err := commandOutput(ctx, "systemctl", "is-active", "--quiet", name)
	return err
}

type coreUpdater struct {
	client   *http.Client
	check    func(context.Context, string) (string, error)
	validate func(context.Context, string, string) error
	services serviceManager
}

func newCoreUpdater() *coreUpdater {
	return &coreUpdater{client: newHTTPClient(90 * time.Second), check: CheckCore, validate: checkConfigs, services: systemServices{}}
}
func (u *coreUpdater) release(ctx context.Context, version string) (verifiedAsset, error) {
	endpoint := "https://api.github.com/repos/SagerNet/sing-box/releases/latest"
	if version != "" {
		value, err := stableVersion(version)
		if err != nil {
			return verifiedAsset{}, err
		}
		endpoint = "https://api.github.com/repos/SagerNet/sing-box/releases/tags/v" + value
	}
	var release coreRelease
	if err := getJSON(ctx, u.client, endpoint, &release); err != nil {
		return verifiedAsset{}, err
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return verifiedAsset{}, errors.New("仅支持 Linux amd64/arm64")
	}
	return selectAsset(release, runtime.GOARCH)
}

func copyBinary(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (u *coreUpdater) download(ctx context.Context, asset verifiedAsset, destination string) error {
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(directory, ".core-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	archive, err := os.Create(filepath.Join(stage, "core.tar.gz"))
	if err != nil {
		return err
	}
	response, err := httpRequest(ctx, u.client, asset.URL)
	if err != nil {
		archive.Close()
		return err
	}
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(archive, digest), io.LimitReader(response.Body, (256<<20)+1))
	response.Body.Close()
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if size > 256<<20 {
		return errors.New("核心归档超过大小限制")
	}
	if hex.EncodeToString(digest.Sum(nil)) != asset.SHA256 {
		return errors.New("核心归档 SHA256 不匹配")
	}
	in, err := os.Open(filepath.Join(stage, "core.tar.gz"))
	if err != nil {
		return err
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer gz.Close()
	tarReader := tar.NewReader(gz)
	expected := "sing-box-" + asset.Version + "-linux-" + asset.Arch + "/sing-box"
	candidate := filepath.Join(stage, "binary")
	found := false
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Name != expected {
			continue
		}
		if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Size < 0 || header.Size > 256<<20 {
			return errors.New("归档中的核心文件无效")
		}
		out, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
		if err != nil {
			return err
		}
		_, err = io.CopyN(out, tarReader, header.Size)
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		found = true
		break
	}
	if !found {
		return errors.New("归档缺少预期核心文件")
	}
	version, err := u.check(ctx, candidate)
	if err != nil {
		return err
	}
	if version != asset.Version {
		return errors.New("下载核心与发行版本不符")
	}
	return os.Rename(candidate, destination)
}

func checkConfigs(ctx context.Context, root, candidate string) error {
	return validateConfigs(ctx, root, candidate, commandOutput)
}

func validateConfigs(ctx context.Context, root, candidate string, check func(context.Context, string, ...string) ([]byte, error)) error {
	snapshots := map[string][32]byte{}
	for _, name := range []string{"sb.json", "vpngate/server.json", "sbox.json", "vpngate/sbox.json"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if _, err = check(ctx, candidate, "check", "-c", path); err != nil {
			return fmt.Errorf("新核心不兼容 %s，保留当前核心: %w", name, err)
		}
		snapshots[path] = sha256.Sum256(data)
	}
	for path, before := range snapshots {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if sha256.Sum256(data) != before {
			return errors.New("校验期间配置发生变化，请重试更新")
		}
	}
	return nil
}

func (u *coreUpdater) activate(ctx context.Context, root, candidate string, restart bool) (string, error) {
	stage, err := os.MkdirTemp(root, ".core-activate-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	next := filepath.Join(stage, "next")
	if err = copyBinary(candidate, next); err != nil {
		return "", err
	}
	version, err := u.check(ctx, next)
	if err != nil {
		return "", err
	}
	if err = u.validate(ctx, root, next); err != nil {
		return "", err
	}
	var active []string
	if restart {
		active, err = u.services.active(ctx)
		if err != nil {
			return "", err
		}
	}
	current, previous := filepath.Join(root, "sing-box"), filepath.Join(root, "sing-box.previous")
	_, currentErr := os.Stat(current)
	hadCurrent := currentErr == nil
	if currentErr != nil && !errors.Is(currentErr, os.ErrNotExist) {
		return "", currentErr
	}
	if hadCurrent {
		backup := filepath.Join(stage, "previous")
		if err = copyBinary(current, backup); err != nil {
			return "", err
		}
		if err = os.Rename(backup, previous); err != nil {
			return "", err
		}
	}
	if err = os.Rename(next, current); err != nil {
		return "", err
	}
	for _, name := range active {
		if err = u.services.restart(ctx, name); err == nil {
			continue
		}
		rollback := context.Background()
		if hadCurrent {
			restore := filepath.Join(stage, "restore")
			if restoreErr := copyBinary(previous, restore); restoreErr != nil {
				return "", errors.Join(err, restoreErr)
			}
			if restoreErr := os.Rename(restore, current); restoreErr != nil {
				return "", errors.Join(err, restoreErr)
			}
			for _, service := range active {
				_ = u.services.restart(rollback, service)
			}
		} else {
			_ = os.Remove(current)
		}
		return "", fmt.Errorf("新核心服务启动失败，已恢复原核心: %w", err)
	}
	return version, nil
}

func RunCore(ctx context.Context, args []string, output io.Writer) error {
	f := flag.NewFlagSet("core", flag.ContinueOnError)
	f.SetOutput(output)
	root := f.String("root", "/etc/s-box", "configuration directory")
	if err := f.Parse(args); err != nil {
		return err
	}
	args = f.Args()
	if len(args) == 0 {
		return errors.New("缺少核心操作")
	}
	operation := args[0]
	options := flag.NewFlagSet(operation, flag.ContinueOnError)
	options.SetOutput(output)
	version := options.String("version", "", "stable version")
	destination := options.String("output", "", "candidate path")
	noRestart := options.Bool("no-restart", false, "do not restart services")
	format := options.String("format", "json", "output: json or text")
	if err := parseOptions(options, args[1:]); err != nil {
		return err
	}
	if *format != "json" && *format != "text" {
		return errors.New("核心输出支持 --format json 或 text")
	}
	u := newCoreUpdater()
	defer u.client.CloseIdleConnections()
	if operation == "check" {
		current := "未安装"
		if _, err := os.Stat(filepath.Join(*root, "sing-box")); err == nil {
			current, err = u.check(ctx, filepath.Join(*root, "sing-box"))
			if err != nil {
				return err
			}
		}
		asset, err := u.release(ctx, "")
		if err != nil {
			return err
		}
		if *format == "text" {
			_, err := fmt.Fprintf(output, " 当前安装核心：%s\n 最新稳定核心：%s\n", current, asset.Version)
			return err
		}
		return json.NewEncoder(output).Encode(Object{"current": current, "latest_stable": asset.Version, "update_available": current != asset.Version})
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return errors.New("核心安装/更新要求 Linux root")
	}
	return withLock(*root, func() error {
		switch operation {
		case "prepare":
			if *destination == "" {
				return errors.New("需要 --output")
			}
			path, err := filepath.Abs(*destination)
			if err != nil {
				return err
			}
			current, _ := filepath.Abs(filepath.Join(*root, "sing-box"))
			if path == current {
				return errors.New("准备文件不能直接覆盖当前核心")
			}
			asset, err := u.release(ctx, *version)
			if err != nil {
				return err
			}
			if err = u.download(ctx, asset, path); err != nil {
				return err
			}
			_, err = fmt.Fprintln(output, asset.Version)
			return err
		case "activate", "rollback":
			candidate := filepath.Join(*root, "sing-box.previous")
			if operation == "activate" {
				if len(options.Args()) != 1 {
					return errors.New("需要候选核心文件")
				}
				candidate = options.Args()[0]
			}
			installed, err := u.activate(ctx, *root, candidate, !*noRestart)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(output, installed)
			return err
		case "update":
			asset, err := u.release(ctx, *version)
			if err != nil {
				return err
			}
			current := filepath.Join(*root, "sing-box")
			if installed, err := u.check(ctx, current); err == nil && installed == asset.Version {
				if err = u.validate(ctx, *root, current); err != nil {
					return err
				}
				if *format == "text" {
					_, err := fmt.Fprintf(output, " 当前已安装此稳定版本：%s\n", installed)
					return err
				}
				return json.NewEncoder(output).Encode(Object{"version": installed, "changed": false})
			}
			stage, err := os.MkdirTemp(*root, ".core-update-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(stage)
			candidate := filepath.Join(stage, "sing-box")
			if err = u.download(ctx, asset, candidate); err != nil {
				return err
			}
			installed, err := u.activate(ctx, *root, candidate, !*noRestart)
			if err != nil {
				return err
			}
			if *format == "text" {
				_, err := fmt.Fprintf(output, " Sing-box 核心已更新：%s\n", installed)
				return err
			}
			return json.NewEncoder(output).Encode(Object{"version": installed, "changed": true})
		}
		return errors.New("未知核心操作")
	})
}
