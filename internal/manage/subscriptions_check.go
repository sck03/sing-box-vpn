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

	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type subscriptionCheck struct {
	NodeID       string `json:"node_id"`
	Name         string `json:"name"`
	Success      bool   `json:"success"`
	Milliseconds int64  `json:"milliseconds"`
	Error        string `json:"error,omitempty"`
	State        string `json:"state"`
	Engine       string `json:"engine"`
}

// A probe starts an isolated core for one node and makes a real HTTP request
// through that protocol. It never edits selections, gateway state or services.
func probeSubscriptionNode(ctx context.Context, root, core, endpoint string, node sub.Node) error {
	if _, diagnostics, err := sub.Render([]sub.Node{node}, sub.Template{ID: "probe", Client: "sing-box", Version: 1, Mode: "nodes"}, false); err != nil {
		for _, diagnostic := range diagnostics {
			if diagnostic.Stage == "capability" {
				return errors.Join(errProbeUnsupported, err)
			}
		}
		return err
	}
	config := Object{"log": Object{"level": "error"}, "inbounds": []Object{{"type": "mixed", "listen": "127.0.0.1", "listen_port": 0}}, "outbounds": []Object{node.SingBox()}, "route": Object{"final": node.Name, "default_domain_resolver": "bootstrap"}, "dns": Object{"servers": []Object{{"type": "local", "tag": "bootstrap"}}}}
	return probeCoreConfig(ctx, root, core, endpoint, config)
}

func probeCoreConfig(ctx context.Context, root, core, endpoint string, config Object) error {
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
	config["inbounds"] = []Object{{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}
	path := filepath.Join(stage, "probe.json")
	if err := AtomicJSON(path, config); err != nil {
		return err
	}
	args := []string{"run", "-c", path}
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

// A newly listening proxy may briefly return a gateway error before its
// outbound is ready. Retry those responses twice within the caller's budget.
func probeHTTP(ctx context.Context, client *http.Client, endpoint string, exited <-chan struct{}) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	transient := 0
	for {
		select {
		case <-ctx.Done():
			return errors.New("protocol probe timed out or was cancelled")
		case <-exited:
			return errors.New("probe core exited; check node configuration")
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
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
			if response.StatusCode == 502 || response.StatusCode == 503 || response.StatusCode == 504 {
				transient++
				if transient < 3 {
					continue
				}
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
	var target string
	var verification sub.Validation
	var diagnostics []sub.Diagnostic
	var c sub.Config
	var r sub.Runtime
	var local map[string][]sub.Node
	if err := withStartupLock(ctx, root, func() error {
		var err error
		c, r, err = loadSubscriptionState(root)
		if err != nil {
			return err
		}
		c, err = c.OnlyOutput(id)
		if err != nil {
			return err
		}
		c, local, r.Accounts, err = captureSubscriptionSources(ctx, root, c, local)
		return err
	}); err != nil {
		return err
	}
	if err := func() error {
		_, snapshot, previews, err := compilePreparedSubscriptions(ctx, root, core, c, r, local, sub.Available)
		if err != nil {
			return err
		}
		c, err = sub.ResolveResources(c, r.Resources)
		if err != nil {
			return err
		}
		for _, out := range c.Outputs {
			if out.ID == id {
				if _, exists := snapshot.Artifacts[id]; !exists {
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
					verification = snapshot.Validations[id]
				}
			}
		}
		if len(nodes) == 0 || len(nodes) > application.MaxProbeNodes {
			return errors.New("probe output must select 1-64 nodes; use a smaller collection")
		}
		return nil
	}(); err != nil {
		return err
	}
	// Prevent several CLI/browser sessions from spawning unbounded probe cores.
	return withNamedFileLock(filepath.Join(root, "subscriptions"), ".check.lock", func() error {
		results := make([]subscriptionCheck, len(nodes))
		for i, node := range nodes {
			results[i] = subscriptionCheck{NodeID: node.ID, Name: node.Name, State: "not_run", Error: "not started before cancellation"}
		}
		jobs := make(chan int)
		var workers sync.WaitGroup
		for range application.ProbeConcurrency {
			workers.Go(func() {
				for i := range jobs {
					results[i] = checkSubscriptionNode(ctx, root, core, endpoint, nodes[i])
				}
			})
		}
	submit:
		for i := range nodes {
			select {
			case jobs <- i:
			case <-ctx.Done():
				break submit
			}
		}
		close(jobs)
		workers.Wait()
		passed, skipped, outcome := summarizeSubscriptionChecks(results)
		var recordErr error
		if ctx.Err() == nil {
			recordErr = withLock(root, func() error {
				current, state, err := loadSubscriptionState(root)
				if err != nil {
					return err
				}
				state.Record("check", id, fmt.Sprintf("%d/%d protocol HTTP probes succeeded; %d unsupported by sing-box", passed, len(results), skipped), time.Now())
				return writeSubscriptionRuntime(root, current, state)
			})
		}
		if err := jsonEncoder(output).Encode(Object{"outcome": outcome, "interrupted": ctx.Err() != nil, "endpoint": endpoint, "client": target, "artifact_validation": verification, "connectivity_engine": "sing-box", "diagnostics": diagnostics, "passed": passed, "skipped": skipped, "total": len(results), "nodes": results}); err != nil {
			return err
		}
		return errors.Join(ctx.Err(), recordErr)
	})
}
