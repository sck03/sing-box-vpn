package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/manage"
)

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

// Keep the public list separate from cached profiles and their credentials.
type nodeSummary struct {
	Host    string `json:"host"`
	IP      string `json:"ip"`
	Country string `json:"country"`
	Network string `json:"network"`
	Speed   int64  `json:"speed"`
	Ping    int    `json:"ping"`
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
		count := 0
		for _, n := range nodes {
			if n.Country == c.Country {
				count++
			}
		}
		out := make([]nodeSummary, 0, count)
		for _, n := range nodes {
			if n.Country == c.Country && (!c.ResidentialOnly || volunteer(n)) {
				out = append(out, nodeSummary{Host: n.Host, IP: n.IP, Country: n.Country, Network: n.Network, Speed: n.Speed, Ping: n.Ping})
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
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		d.DisallowUnknownFields()
		if e := d.Decode(&a); e != nil {
			respond(w, 400, map[string]string{"error": "invalid action"})
			return
		}
		if e := d.Decode(&struct{}{}); e != io.EOF {
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
		if a.Op == "refresh" {
			if a.Country != "" && strings.ToUpper(a.Country) != c.Country {
				respond(w, 409, map[string]string{"error": "configure that country's independent entries through sb first"})
				return
			}
			m.scheduleRefresh(parent, c, true)
			respond(w, 202, m.snapshot())
			return
		}
		swap := false
		code := 400
		root := filepath.Dir(m.configPath)
		if filepath.Base(root) == "vpngate" {
			root = filepath.Dir(root)
		}
		e := manage.ConfigTransaction(root, func() error {
			// Menu configuration may have changed while the daemon still holds its
			// previous snapshot. Merge this action into the latest published file.
			latest, err := readConfig(m.configPath)
			if err == nil {
				c = latest
			} else if !os.IsNotExist(err) || a.Op == "reload" {
				return err
			}
			if a.Country != "" && strings.ToUpper(a.Country) != c.Country {
				code = 409
				return errors.New("configure that country's independent entries through sb first")
			}
			switch a.Op {
			case "start":
				c.Enabled = true
			case "stop":
				c.Enabled = false
			case "swap":
				if c.Mode == "fixed" {
					return errors.New("switch to sticky mode before swapping")
				}
				if s.State == "connecting" || s.State == "verifying" {
					return errors.New("a tunnel is already connecting")
				}
				c.Enabled = true
				swap = true
			case "mode":
				if a.Mode != "sticky" && a.Mode != "fixed" {
					return errors.New("mode must be sticky or fixed")
				}
				c.Mode, c.FixedHost = a.Mode, a.FixedHost
				if c.Mode == "fixed" && c.FixedHost == "" && s.Country == c.Country {
					c.FixedHost = s.Node
				}
				if c.Mode == "sticky" {
					c.FixedHost = ""
				}
			case "reload":
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
			default:
				return errors.New("unknown action")
			}
			return m.apply(c, swap)
		})
		if e != nil {
			respond(w, code, map[string]string{"error": e.Error()})
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
	client := &http.Client{Timeout: *requestTimeout, Transport: transport, CheckRedirect: rejectHTTPRedirect}
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
		body = bytes.NewReader(b)
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
	b, e := readHTTPBody(res.Body, 1<<20)
	if e != nil {
		return e
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var reply struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(b, &reply) == nil && strings.TrimSpace(reply.Error) != "" {
			return errors.New(reply.Error)
		}
		return fmt.Errorf("control operation failed: HTTP response %d", res.StatusCode)
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
