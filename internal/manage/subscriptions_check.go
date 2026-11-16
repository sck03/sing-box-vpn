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
	"sync"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type subscriptionCheck struct {
	NodeID       string `json:"node_id"`
	Name         string `json:"name"`
	Success      bool   `json:"success"`
	Milliseconds int64  `json:"milliseconds"`
	Error        string `json:"error,omitempty"`
}

// A probe starts an isolated core for one node and makes a real HTTP request
// through that protocol. It never edits selections, gateway state or services.
func probeSubscriptionNode(ctx context.Context, root, core, endpoint string, node sub.Node) error {
	if _, _, err := sub.Render([]sub.Node{node}, sub.Template{ID: "probe", Client: "sing-box", Version: 1, Mode: "nodes"}, false); err != nil {
		return err
	}
	config := Object{"log": Object{"level": "error"}, "inbounds": []Object{{"type": "mixed", "listen": "127.0.0.1", "listen_port": 0}}, "outbounds": []Object{node.SingBox()}, "route": Object{"final": node.Name, "default_domain_resolver": "bootstrap"}, "dns": Object{"servers": []Object{{"type": "local", "tag": "bootstrap"}}}}
	return probeCoreConfig(ctx, root, core, endpoint, config)
}

func probeCoreConfig(ctx context.Context, root, core, endpoint string, config Object) error {
	child, cancel := context.WithTimeout(ctx, 12*time.Second)
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
	config["inbounds"] = []Object{{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}
	path := filepath.Join(stage, "probe.json")
	if err := AtomicJSON(path, config); err != nil {
		return err
	}
	cmd := exec.CommandContext(child, core, "run", "-c", path)
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
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-child.Done():
			return errors.New("protocol probe timed out or was cancelled")
		case <-done:
			return errors.New("probe core exited; check node configuration")
		case <-ticker.C:
			req, err := http.NewRequestWithContext(child, http.MethodGet, endpoint, nil)
			if err != nil {
				return err
			}
			response, err := client.Do(req)
			if err != nil {
				continue
			}
			n, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, (1<<20)+1))
			response.Body.Close()
			if readErr != nil || n > 1<<20 {
				return errors.New("probe response interrupted or oversized")
			}
			if response.StatusCode < 200 || response.StatusCode >= 400 {
				return fmt.Errorf("probe endpoint HTTP %d", response.StatusCode)
			}
			return nil
		}
	}
}

func checkSubscriptionOutput(ctx context.Context, root, core, id, endpoint string, output io.Writer) error {
	if !sub.ValidID(id) {
		return errors.New("check requires --id")
	}
	if err := sub.HTTPURL(endpoint); err != nil {
		return err
	}
	var nodes []sub.Node
	var target, verification string
	var diagnostics []sub.Diagnostic
	if err := withStartupLock(ctx, root, func() error {
		c, r, local, err := loadSubscriptions(root)
		if err != nil {
			return err
		}
		_, snapshot, previews, err := compileSubscriptions(ctx, root, core, c, r, local, true)
		if err != nil {
			return err
		}
		c, err = sub.ResolveResources(c, r.Resources)
		if err != nil {
			return err
		}
		for _, out := range c.Outputs {
			if out.ID == id {
				artifact, exists := snapshot.Artifacts[id]
				if !exists {
					return errors.New("selected output is unavailable")
				}
				for _, template := range c.Templates {
					if template.ID != out.Template {
						continue
					}
					target = template.Client
					nodes, diagnostics, err = sub.Select(previews[out.Collection].Nodes, template, out.SkipUnsupported)
					if err != nil {
						return err
					}
					verification, err = validateSubscriptionArtifact(ctx, root, core, template, artifact)
					if err != nil {
						return err
					}
				}
			}
		}
		if len(nodes) == 0 || len(nodes) > 64 {
			return errors.New("probe output must select 1-64 nodes; use a smaller collection")
		}
		return nil
	}); err != nil {
		return err
	}
	// Prevent several CLI/browser sessions from spawning unbounded probe cores.
	return withNamedFileLock(filepath.Join(root, "subscriptions"), ".check.lock", func() error {
		results := make([]subscriptionCheck, len(nodes))
		jobs := make(chan int)
		var workers sync.WaitGroup
		for range 2 {
			workers.Go(func() {
				for i := range jobs {
					start := time.Now()
					err := probeSubscriptionNode(ctx, root, core, endpoint, nodes[i])
					result := subscriptionCheck{NodeID: nodes[i].ID, Name: nodes[i].Name, Success: err == nil, Milliseconds: time.Since(start).Milliseconds()}
					if err != nil {
						result.Error = err.Error()
					}
					results[i] = result
				}
			})
		}
		for i := range nodes {
			select {
			case jobs <- i:
			case <-ctx.Done():
			}
		}
		close(jobs)
		workers.Wait()
		if err := ctx.Err(); err != nil {
			return err
		}
		passed := 0
		for _, result := range results {
			if result.Success {
				passed++
			}
		}
		if err := withLock(root, func() error {
			_, state, _, err := loadSubscriptions(root)
			if err != nil {
				return err
			}
			state.Record("check", id, fmt.Sprintf("%d/%d protocol HTTP probes succeeded", passed, len(results)), time.Now())
			return AtomicJSON(filepath.Join(root, subscriptionsRuntime), state)
		}); err != nil {
			return err
		}
		return jsonEncoder(output).Encode(Object{"endpoint": endpoint, "client": target, "artifact_validation": verification, "connectivity_engine": "sing-box", "diagnostics": diagnostics, "passed": passed, "total": len(results), "nodes": results})
	})
}
