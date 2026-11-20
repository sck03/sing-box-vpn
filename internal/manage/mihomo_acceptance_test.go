package manage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// Client processes are test fixtures; none of this code enters server binaries.
func probeMihomoNode(ctx context.Context, root, endpoint string, node sub.Node) error {
	node.Name = "probe-target"
	config := Object{"log-level": "silent", "mode": "rule", "allow-lan": false, "bind-address": "127.0.0.1", "proxies": []Object{node.Mihomo()}, "rules": []string{"MATCH," + node.Name}}
	return probeMihomoConfig(ctx, root, os.Getenv("MIHOMO_BINARY"), endpoint, config)
}

func probeMihomoConfig(ctx context.Context, root, core, endpoint string, config Object) error {
	child, cancel := context.WithTimeout(ctx, application.ProbeTimeout)
	defer cancel()
	stage, err := os.MkdirTemp(root, ".probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	port, err := AvailablePort(map[int]bool{})
	if err != nil {
		return err
	}
	config["mixed-port"] = port
	path := filepath.Join(stage, "probe.json")
	if err := AtomicJSON(path, config); err != nil {
		return err
	}
	args := []string{"-d", stage, "-f", path}
	cmd := exec.CommandContext(child, core, args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = stage, io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return errors.New("probe core could not start")
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	defer func() { cancel(); <-done }()
	proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return probeHTTP(child, client, endpoint, done)
}
