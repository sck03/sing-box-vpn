package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	project "github.com/sck03/sing-box-vpn"
	"github.com/sck03/sing-box-vpn/internal/manage"
)

func main() {
	if err := build(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func build() error {
	rootArg := flag.String("root", ".", "repository root")
	flag.Parse()
	root, err := filepath.Abs(*rootArg)
	if err != nil {
		return err
	}
	goBinary := os.Getenv("GO_BINARY")
	if goBinary == "" {
		goBinary = "go"
	}
	version, err := exec.Command(goBinary, "version").Output()
	if err != nil {
		return err
	}
	if !strings.Contains(string(version), "go"+project.Versions.Go+" ") {
		return errors.New("build requires Go " + project.Versions.Go)
	}
	destination := filepath.Join(root, ".build", "release")
	for _, arch := range []string{"amd64", "arm64"} {
		binary := filepath.Join(destination, ".bin", "linux-"+arch, "yg-vpngate")
		if err := os.MkdirAll(filepath.Dir(binary), 0755); err != nil {
			return err
		}
		cmd := exec.Command(goBinary, "build", "-trimpath", "-ldflags", "-s -w -X main.version="+strings.TrimPrefix(project.Versions.Release, "v"), "-o", binary, "./addons/vpngate/gateway")
		cmd.Dir = root
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "GOOS=") && !strings.HasPrefix(value, "GOARCH=") && !strings.HasPrefix(value, "CGO_ENABLED=") {
				cmd.Env = append(cmd.Env, value)
			}
		}
		cmd.Env = append(cmd.Env, "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
		if err = cmd.Run(); err != nil {
			return err
		}
		if err = os.Chmod(binary, 0755); err != nil {
			return err
		}
	}
	archive := filepath.Join(destination, "sing-box-vpn-"+project.Versions.Release+".tar.gz")
	file, err := os.CreateTemp(destination, ".release-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	digest := sha256.New()
	gz := gzip.NewWriter(io.MultiWriter(file, digest))
	writer := tar.NewWriter(gz)
	add := func(path, prefix string) error {
		var files []string
		err := filepath.WalkDir(path, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			if strings.HasSuffix(name, ".pyc") {
				return nil
			}
			files = append(files, name)
			return nil
		})
		if err != nil {
			return err
		}
		sort.Strings(files)
		for _, name := range files {
			info, err := os.Lstat(name)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return errors.New("release must not contain links")
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return errors.New("release must contain regular files")
			}
			relative, err := filepath.Rel(path, name)
			if err != nil {
				return err
			}
			archiveName := prefix
			if relative != "." {
				archiveName += "/" + filepath.ToSlash(relative)
			}
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = "sing-box-vpn/" + archiveName
			header.Uid, header.Gid = 0, 0
			header.Uname, header.Gname = "root", "root"
			header.ModTime, header.AccessTime, header.ChangeTime = time.Unix(0, 0), time.Time{}, time.Time{}
			header.Mode = 0644
			if info.IsDir() {
				header.Mode = 0755
			}
			if strings.HasSuffix(name, ".sh") || filepath.Base(name) == "yg-vpngate" {
				header.Mode = 0755
			}
			if err = writer.WriteHeader(header); err != nil {
				return err
			}
			if !info.IsDir() {
				in, err := os.Open(name)
				if err != nil {
					return err
				}
				_, err = io.Copy(writer, in)
				in.Close()
				if err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, member := range []string{"sb.sh", "install.sh", "lib", "addons", "internal", "cmd", "scripts", "go.mod", "manifest.go", "versions.json", "LICENSE", "NOTICE.md", "README.md", "VPNGate-实施方案.md", "docs", ".github"} {
		if err = add(filepath.Join(root, member), member); err != nil {
			writer.Close()
			gz.Close()
			file.Close()
			return err
		}
	}
	if err = add(filepath.Join(destination, ".bin"), ".bin"); err != nil {
		writer.Close()
		gz.Close()
		file.Close()
		return err
	}
	if err = writer.Close(); err == nil {
		err = gz.Close()
	} else {
		gz.Close()
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(file.Name(), archive); err != nil {
		return err
	}
	hash := hex.EncodeToString(digest.Sum(nil))
	if err = manage.AtomicBytes(filepath.Join(destination, "SHA256SUMS"), []byte(hash+"  "+filepath.Base(archive)+"\n"), 0644); err != nil {
		return err
	}
	fmt.Println(filepath.Base(archive) + " " + hash)
	return nil
}
