package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	project "github.com/sck03/sing-box-vpn"
	"github.com/sck03/sing-box-vpn/internal/manage"
)

var version string

func init() {
	if version == "" {
		version = strings.TrimPrefix(project.Versions.Release, "v")
	}
}

const defaultConfig = "/etc/s-box/vpngate/gateway.json"

type action struct {
	Op             string   `json:"op"`
	Country        string   `json:"country,omitempty"`
	Mode           string   `json:"mode,omitempty"`
	FixedHost      string   `json:"fixed_host,omitempty"`
	Sources        []string `json:"sources,omitempty"`
	RequireISP     *bool    `json:"require_isp,omitempty"`
	ExcludeHosting *bool    `json:"exclude_hosting,omitempty"`
	AllowASNs      []int    `json:"allow_asns,omitempty"`
	ClearASNs      bool     `json:"clear_asns,omitempty"`
}

func main() {
	if e := entry(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "yg-vpngate:", e)
		os.Exit(1)
	}
}
func entry(args []string) error {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Println("yg-vpngate daemon|ctl|worker|cleanup|config|core|firewall|asset|remote|subscription|--version\nctl: status, nodes, start, stop, swap, reload, refresh, mode, policy")
		return nil
	}
	if args[0] == "--version" {
		fmt.Println(version)
		return nil
	}
	switch args[0] {
	case "config", "core", "firewall", "asset", "remote", "subscription":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		switch args[0] {
		case "config":
			return manage.RunConfig(ctx, args[1:], os.Stdout)
		case "core":
			return manage.RunCore(ctx, args[1:], os.Stdout)
		case "firewall":
			return manage.RunFirewall(ctx, args[1:], os.Stdout)
		case "asset":
			return manage.RunAsset(ctx, args[1:], os.Stdout)
		case "remote":
			return manage.RunRemote(ctx, args[1:], os.Stdin, os.Stdout)
		case "subscription":
			return manage.RunSubscription(ctx, args[1:], os.Stdout)
		}
		return errors.New("unknown management command")
	case "daemon":
		f := flag.NewFlagSet("daemon", flag.ContinueOnError)
		path := f.String("config", defaultConfig, "configuration file")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		return runDaemon(*path)
	case "worker":
		f := flag.NewFlagSet("worker", flag.ContinueOnError)
		socket := f.String("socket", "", "private Unix socket")
		dns := f.String("dns", "1.1.1.1:53", "tunnel DNS")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		if runtime.GOOS != "linux" {
			return errors.New("worker is a Linux network-namespace process")
		}
		if *socket == "" {
			return errors.New("worker socket missing")
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return (&worker{socket: *socket, dns: *dns}).serve(ctx)
	case "ctl":
		return runCTL(args[1:])
	case "cleanup":
		f := flag.NewFlagSet("cleanup", flag.ContinueOnError)
		path := f.String("config", defaultConfig, "configuration file")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		c, e := readConfig(*path)
		if e != nil {
			return e
		}
		if e = preflight(); e != nil {
			if runtime.GOOS != "linux" || os.Geteuid() != 0 {
				return e
			}
		}
		if daemonAlive(c.DataDir) {
			return errors.New("stop the gateway daemon before cleanup")
		}
		unlock, lockErr := lockDaemon(c.DataDir)
		if os.IsNotExist(lockErr) {
			return nil
		}
		if lockErr != nil {
			return lockErr
		}
		defer unlock()
		b := &backend{cfg: c}
		data, e := os.ReadFile(filepath.Join(c.DataDir, "namespace-owner.json"))
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if json.Unmarshal(data, &b.owner) != nil || b.owner.Owner != ownerID {
			return errors.New("resource ownership cannot be verified")
		}
		b.claimed = true
		b.stop()
		return b.cleanupErr
	default:
		return errors.New("unknown command")
	}
}

func daemonAlive(dir string) bool {
	f, e := os.OpenFile(filepath.Join(dir, "daemon.lock"), os.O_RDWR, 0600)
	if os.IsNotExist(e) {
		return false
	}
	if e != nil {
		return true
	}
	defer f.Close()
	return manage.TryLockFile(f) != nil
}
func lockDaemon(dir string) (func(), error) {
	path := filepath.Join(dir, "daemon.lock")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = manage.TryLockFile(f); e != nil {
		f.Close()
		return nil, errors.New("another gateway daemon owns this directory")
	}
	if e = f.Truncate(0); e == nil {
		_, e = fmt.Fprintf(f, "%d\n", os.Getpid())
	}
	if e != nil {
		f.Close()
		return nil, e
	}
	return func() { f.Close() }, nil
}

func runDaemon(path string) error {
	c, e := readConfig(path)
	if e != nil {
		return e
	}
	if e = preflight(); e != nil {
		return e
	}
	for _, d := range []string{c.DataDir, c.RunDir} {
		if e = os.MkdirAll(d, 0700); e != nil {
			return e
		}
		if e = os.Chmod(d, 0700); e != nil {
			return e
		}
	}
	unlock, e := lockDaemon(c.DataDir)
	if e != nil {
		return e
	}
	defer unlock()
	socket := filepath.Join(c.RunDir, "control.sock")
	_ = os.Remove(socket)
	control, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	defer control.Close()
	defer os.Remove(socket)
	if e = os.Chmod(socket, 0600); e != nil {
		return e
	}
	proxy, e := net.Listen("tcp4", c.Listen)
	if e != nil {
		return e
	}
	defer proxy.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	m := newManager(c, path)
	api := http.Server{Handler: m.handler(ctx), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan struct{})
	serveErrors := make(chan error, 2)
	go func() { defer close(done); m.run(ctx) }()
	go func() {
		if e := api.Serve(control); e != nil && !errors.Is(e, http.ErrServerClosed) {
			serveErrors <- fmt.Errorf("control listener failed: %w", e)
			cancel()
		}
	}()
	go func() {
		if e := serveProxy(ctx, proxy, m); e != nil {
			serveErrors <- fmt.Errorf("proxy listener failed: %w", e)
			cancel()
		}
	}()
	<-ctx.Done()
	m.revoke()
	_ = api.Close()
	<-done
	select {
	case e := <-serveErrors:
		return e
	default:
		return nil
	}
}

func (m *manager) handler(parent context.Context) http.Handler {
	mux := http.NewServeMux()
	respond := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		respond(w, 200, m.snapshot())
	})
	mux.HandleFunc("/v1/nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		m.mu.Lock()
		c := m.cfg
		nodes := m.nodes
		m.mu.Unlock()
		var out []map[string]any
		for _, n := range nodes {
			if n.Country == c.Country && (!c.ResidentialOnly || volunteer(n)) {
				out = append(out, map[string]any{"host": n.Host, "ip": n.IP, "country": n.Country, "network": n.Network, "speed": n.Speed, "ping": n.Ping})
			}
		}
		respond(w, 200, out)
	})
	mux.HandleFunc("/v1/actions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var a action
		d := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
		d.DisallowUnknownFields()
		if e := d.Decode(&a); e != nil {
			respond(w, 400, map[string]string{"error": "invalid action"})
			return
		}
		// Serialize the snapshot and apply together so concurrent actions cannot
		// overwrite settings changed by another request.
		m.actionsMu.Lock()
		defer m.actionsMu.Unlock()
		m.mu.Lock()
		c := m.cfg
		s := m.status
		m.mu.Unlock()
		var e error
		swap := false
		if a.Country != "" && strings.ToUpper(a.Country) != c.Country {
			respond(w, 409, map[string]string{"error": "configure that country's independent entries through sb first"})
			return
		}
		switch a.Op {
		case "start":
			c.Enabled = true
		case "stop":
			c.Enabled = false
		case "swap":
			if c.Mode == "fixed" {
				e = errors.New("switch to sticky mode before swapping")
			}
			if s.State == "connecting" || s.State == "verifying" {
				e = errors.New("a tunnel is already connecting")
			}
			c.Enabled = true
			swap = true
		case "mode":
			if a.Mode != "sticky" && a.Mode != "fixed" {
				e = errors.New("mode must be sticky or fixed")
			} else {
				c.Mode = a.Mode
				c.FixedHost = a.FixedHost
				if c.Mode == "fixed" && c.FixedHost == "" {
					c.FixedHost = s.Node
				}
				if c.Mode == "sticky" {
					c.FixedHost = ""
				}
			}
		case "reload":
			c, e = readConfig(m.configPath)
		case "policy":
			if a.RequireISP != nil {
				c.RequireISP = *a.RequireISP
			}
			if a.ExcludeHosting != nil {
				c.ExcludeHosting = *a.ExcludeHosting
			}
			if a.Sources != nil {
				c.Sources = a.Sources
			}
			if a.AllowASNs != nil || a.ClearASNs {
				c.AllowASNs = a.AllowASNs
			}
		case "refresh":
			m.scheduleRefresh(parent, c, true)
			respond(w, 202, m.snapshot())
			return
		default:
			e = errors.New("unknown action")
		}
		if e == nil {
			e = m.apply(c, swap)
		}
		if e != nil {
			respond(w, 400, map[string]string{"error": e.Error()})
			return
		}
		respond(w, 202, m.snapshot())
	})
	return mux
}

func runCTL(args []string) error {
	return runCTLWithOutput(args, os.Stdout)
}

func runCTLWithOutput(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("ctl operation missing")
	}
	op := args[0]
	f := flag.NewFlagSet("ctl "+op, flag.ContinueOnError)
	f.SetOutput(output)
	path := f.String("config", defaultConfig, "configuration file")
	country := f.String("country", "", "configured country")
	mode := f.String("mode", "", "sticky or fixed")
	fixed := f.String("node", "", "fixed node")
	sources := f.String("sources", "", "comma-separated source URLs")
	asns := f.String("asns", "", "comma-separated ISP ASN allowlist")
	require := f.Bool("require-isp", false, "require confirmed ISP classification")
	hosting := f.Bool("exclude-hosting", true, "reject known hosting exits")
	format := f.String("format", "json", "status output: json or text")
	requestTimeout := f.Duration("timeout", 10*time.Second, "local control request timeout")
	if e := f.Parse(args[1:]); e != nil {
		return e
	}
	if *format != "json" && (*format != "text" || op != "status") {
		return errors.New("status 支持 --format json 或 text")
	}
	if *requestTimeout <= 0 || *requestTimeout > time.Minute {
		return errors.New("control timeout must be between 0 and 1 minute")
	}
	c, e := readConfig(*path)
	if e != nil {
		return e
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _ string, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", filepath.Join(c.RunDir, "control.sock"))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: *requestTimeout, Transport: transport}
	method, url := http.MethodGet, "http://unix/v1/"+op
	var body io.Reader
	if op != "status" && op != "nodes" {
		a := action{Op: op, Country: *country, Mode: *mode, FixedHost: *fixed}
		if *sources != "" {
			a.Sources = strings.Split(*sources, ",")
		}
		if *asns != "" {
			for _, s := range strings.Split(*asns, ",") {
				n, e := strconv.Atoi(strings.TrimSpace(s))
				if e != nil || n <= 0 {
					return errors.New("invalid ASN allowlist")
				}
				a.AllowASNs = append(a.AllowASNs, n)
			}
		}
		f.Visit(func(v *flag.Flag) {
			if v.Name == "asns" && *asns == "" {
				a.ClearASNs = true
			}
			if v.Name == "require-isp" {
				a.RequireISP = require
			}
			if v.Name == "exclude-hosting" {
				a.ExcludeHosting = hosting
			}
		})
		b, _ := json.Marshal(a)
		body = strings.NewReader(string(b))
		method = http.MethodPost
		url = "http://unix/v1/actions"
	}
	req, e := http.NewRequest(method, url, body)
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := client.Do(req)
	if e != nil {
		if op == "status" && *format == "text" {
			// A saved state.json may outlive the daemon; never present it as a live exit.
			return writeGatewayStatus(output, Status{State: "unavailable", Country: c.Country, Mode: c.Mode})
		}
		return errors.New("gateway control socket unavailable; check yg-vpngate service")
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if e != nil {
		return e
	}
	if res.StatusCode >= 400 {
		var v map[string]string
		if json.Unmarshal(b, &v) == nil {
			return errors.New(v["error"])
		}
		return errors.New("control operation failed")
	}
	if op == "status" && *format == "text" {
		var status Status
		if e := json.Unmarshal(b, &status); e != nil {
			return e
		}
		return writeGatewayStatus(output, status)
	}
	_, e = output.Write(b)
	return e
}
