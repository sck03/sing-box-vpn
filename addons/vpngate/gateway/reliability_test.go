package main

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/manage"
)

func TestUnchangedConfigKeepsVerifiedTunnel(t *testing.T) {
	cfg := defaults()
	cfg.DataDir, cfg.RunDir = t.TempDir(), t.TempDir()
	cfg.ProxyUser, cfg.ProxyPass = "test", strings.Repeat("a", 64)
	cfg.Enabled = true
	path := filepath.Join(cfg.DataDir, "config.json")
	if err := atomicJSON(path, cfg); err != nil {
		t.Fatal(err)
	}
	m := newManager(cfg, path)
	defer m.sourceClient.CloseIdleConnections()
	m.ready = true
	m.current = &fakeTunnel{dead: make(chan error)}
	var cancelled atomic.Bool
	m.cancel = func() { cancelled.Store(true) }
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	m.track(client)
	ticket, _ := m.authenticate(cfg.ProxyUser, cfg.ProxyPass)
	cfg.Sources = append([]string(nil), cfg.Sources...)
	if err := m.apply(cfg, false); err != nil {
		t.Fatal(err)
	}
	if cancelled.Load() || !m.allowed(ticket) || len(m.connections) != 1 || len(m.wake) != 0 {
		t.Fatal("repeated start/reload revoked an unchanged verified tunnel")
	}
}

func TestPinnedNodeUsesRefreshedProfile(t *testing.T) {
	old := Node{Host: "jp-node", IP: "203.0.113.5", Country: "JP", Profile: safeTestProfile}
	fresh := old
	fresh.IP = "203.0.113.6"
	fresh.Profile = strings.ReplaceAll(safeTestProfile, old.IP, fresh.IP)
	for _, mode := range []string{"fixed", "sticky"} {
		cfg := defaults()
		cfg.Mode = mode
		if mode == "fixed" {
			cfg.FixedHost = old.Host
		}
		pool := candidates(cfg, []Node{fresh}, old, false, nil)
		if len(pool) != 1 || pool[0].IP != fresh.IP {
			t.Fatalf("%s kept stale cached node after refresh: %+v", mode, pool)
		}
	}
}

func TestWorkerCancellationClosesActiveTCPRelay(t *testing.T) {
	client, control := net.Pipe()
	remote, endpoint := net.Pipe()
	defer client.Close()
	defer control.Close()
	defer remote.Close()
	defer endpoint.Close()
	w := &worker{dialOverride: func(context.Context, string, string) (net.Conn, error) { return remote, nil }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); w.handle(ctx, control) }()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	if err := writeObject(client, dialRequest{Network: "tcp", Address: "203.0.113.5:80"}); err != nil {
		t.Fatal(err)
	}
	if _, err := readFrame(client, 4096); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled worker retained the active relay and its target socket")
	}
}

func TestRealDomainRoutingPreservesAddressFallback(t *testing.T) {
	core := os.Getenv("SING_BOX_BINARY")
	if core == "" {
		t.Skip("set SING_BOX_BINARY for real domain routing")
	}
	dns, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer dns.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := dns.ReadFrom(buf)
			if err != nil {
				return
			}
			answer := dnsTestAnswer(buf[:n], false)
			if answer == nil {
				continue
			}
			questionEnd := len(answer) - 16
			if binary.BigEndian.Uint16(answer[questionEnd-4:questionEnd-2]) == 1 {
				copy(answer[len(answer)-4:], []byte{127, 0, 0, 1})
			} else {
				answer = answer[:questionEnd]
				clear(answer[6:8]) // The test name has no AAAA record.
			}
			_, _ = dns.WriteTo(answer, addr)
		}
	}()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "IPv4 target") }))
	defer target.Close()
	_, targetPort, _ := net.SplitHostPort(target.Listener.Addr().String())
	_, dnsPort, _ := net.SplitHostPort(dns.LocalAddr().String())
	dnsNumber, _ := strconv.Atoi(dnsPort)
	for _, channel := range []string{"direct4", "direct6"} {
		t.Run(channel, func(t *testing.T) {
			root := t.TempDir()
			port, err := manage.AvailablePort(map[int]bool{})
			if err != nil {
				t.Fatal(err)
			}
			server, err := manage.ServerConfig([]manage.Object{{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, map[string][]string{channel: {"ipv4-only.test"}}, nil, 40000)
			if err != nil {
				t.Fatal(err)
			}
			server["dns"] = manage.Object{"servers": []manage.Object{{"type": "udp", "tag": "test-dns", "server": "127.0.0.1", "server_port": dnsNumber}}}
			server["route"].(manage.Object)["default_domain_resolver"] = "test-dns"
			protocolCore(t, root, "domain-route", core, server, port)
			proxyURL, _ := url.Parse("socks5://127.0.0.1:" + strconv.Itoa(port))
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			for _, address := range []string{"http://ipv4-only.test:" + targetPort + "/", target.URL} {
				request, err := http.NewRequest(http.MethodGet, address, nil)
				if err != nil {
					t.Fatal(err)
				}
				request.Host = "ipv4-only.test"
				response, err := client.Do(request)
				if err != nil {
					t.Fatal("available address fallback failed", channel, address, err)
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || string(body) != "IPv4 target" {
					t.Fatal("domain route lost target response", err)
				}
			}
		})
	}
}

func TestProbeRejectsUnknownHostExit(t *testing.T) {
	cfg := defaults()
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	m.hostClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == cfg.ProbeURL {
			return testHTTPResponse("not an IP address"), nil
		}
		return testHTTPResponse(`{"country_code":"JP","hosting":false}`), nil
	})
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return testHTTPResponse("203.0.113.5"), nil })}
	if _, err := m.probe(context.Background(), cfg, &fakeTunnel{}, client); err == nil || !strings.Contains(err.Error(), "host exit") {
		t.Fatal("unknown VPS exit was treated as verified", err)
	}
}

func TestGeoRejectsMetadataForAnotherIP(t *testing.T) {
	if _, err := parseGeo([]byte(`{"ip":"203.0.113.6","country_code":"JP"}`), "203.0.113.5"); err == nil {
		t.Fatal("country metadata for a different address was accepted")
	}
}

func TestTransientHealthFailureRechecksExistingTunnel(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	cfg.HealthInterval = 1
	cfg.ProxyUser, cfg.ProxyPass = "test", strings.Repeat("a", 64)
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	active := &fakeTunnel{dead: make(chan error)}
	m.current, m.ready = active, true
	oldTicket, _ := m.authenticate(cfg.ProxyUser, cfg.ProxyPass)
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			response := testHTTPResponse("temporary upstream failure")
			response.StatusCode = 503
			return response, nil
		}
		return testHTTPResponse("203.0.113.5"), nil
	})}
	m.hostClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == cfg.ProbeURL {
			return testHTTPResponse("203.0.113.6"), nil
		}
		return testHTTPResponse(`{"country_code":"JP","hosting":false}`), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.monitor(ctx, cfg, active, Node{Host: "jp-node"}, client) }()
	deadline := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatal("temporary lookup failure abandoned the existing tunnel", err)
		default:
		}
		if !m.allowed(oldTicket) {
			blocked = true
		}
		newTicket, _ := m.authenticate(cfg.ProxyUser, cfg.ProxyPass)
		if blocked && requests.Load() >= 2 && m.allowed(newTicket) {
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("forwarding was not blocked and restored after successful revalidation")
}

func TestHealthRecheckBlocksInvalidCountryWithoutRetry(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	m.ready = true
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return testHTTPResponse("203.0.113.5"), nil
	})}
	m.hostClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == cfg.ProbeURL {
			return testHTTPResponse("203.0.113.6"), nil
		}
		return testHTTPResponse(`{"country_code":"KR","hosting":false}`), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := m.recheck(ctx, cfg, &fakeTunnel{}, Node{}, client); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatal("wrong-country exit was not rejected immediately", err)
	}
	if requests.Load() != 1 || m.ready {
		t.Fatal("wrong-country verification was retried or forwarding stayed enabled")
	}
}
