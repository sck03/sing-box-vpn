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
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/manage"
)

type action struct {
	Op      string `json:"op"`
	Country string `json:"country,omitempty"`
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
		// Keep snapshot and apply serial, but let abandoned requests and daemon
		// shutdown leave the queue without waiting for another disk commit.
		select {
		case m.actionSlot <- struct{}{}:
			defer func() { <-m.actionSlot }()
		case <-r.Context().Done():
		case <-parent.Done():
		}
		if r.Context().Err() != nil || parent.Err() != nil {
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "control action cancelled"})
			return
		}
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
			if err := r.Context().Err(); err != nil {
				return err
			}
			if err := parent.Err(); err != nil {
				return err
			}
			// Reload the latest projection published by the platform.
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
			case "start", "stop":
				return errors.New("change persistent gateway state through panel tasks")
			case "swap":
				if !c.Enabled {
					return errors.New("start the gateway before swapping")
				}
				if c.Mode == "fixed" {
					return errors.New("switch to sticky mode before swapping")
				}
				if s.State == "connecting" || s.State == "verifying" {
					return errors.New("a tunnel is already connecting")
				}
				swap = true
			case "mode":
				return errors.New("edit gateway policy through panel configuration")
			case "reload":
			case "policy":
				return errors.New("edit gateway policy through panel configuration")
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
		a := action{Op: op, Country: *country}
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
