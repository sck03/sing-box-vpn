package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/manage"
)

func protocolCore(t *testing.T, root, name, binary string, config manage.Object, port int) {
	t.Helper()
	path := filepath.Join(root, name+".json")
	if err := manage.CheckJSON(context.Background(), binary, root, config); err != nil {
		t.Fatal(err)
	}
	if err := manage.AtomicJSON(path, config); err != nil {
		t.Fatal(err)
	}
	protocolProcess(t, root, name, exec.Command(binary, "run", "-c", path), port)
}

func protocolProcess(t *testing.T, root, name string, cmd *exec.Cmd, port int) {
	t.Helper()
	logPath := filepath.Join(root, name+".log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			_ = cmd.Process.Kill()
			<-done
		}
		log.Close()
		if t.Failed() {
			data, _ := os.ReadFile(logPath)
			if len(data) > 4096 {
				data = data[len(data)-4096:]
			}
			t.Logf("%s log:\n%s", name, data)
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case <-done:
			data, _ := os.ReadFile(logPath)
			t.Fatalf("%s failed: %s", name, data)
		default:
		}
		conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(name + " did not listen")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func protocolUDP(t *testing.T, prefix string) string {
	t.Helper()
	socket, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { socket.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := socket.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = socket.WriteTo(append([]byte(prefix), buf[:n]...), addr)
		}
	}()
	return socket.LocalAddr().String()
}

func protocolMihomoReady(t *testing.T, port int, target string) {
	t.Helper()
	// Mihomo opens its listener while routing is still suspended during provider
	// loading. A successful HTTP request marks readiness, not merely an open port.
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := client.Get("http://" + target)
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if err == nil && response.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("Mihomo routing did not become ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func protocolMixed(t *testing.T, port int, command byte, target string) (net.Conn, string) {
	t.Helper()
	c, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	_, _ = c.Write([]byte{5, 1, 0})
	var greeting [2]byte
	if _, err = io.ReadFull(c, greeting[:]); err != nil || greeting != [2]byte{5, 0} {
		t.Fatal("mixed greeting", err)
	}
	_, _ = c.Write(requestAddress(command, target))
	var reply [4]byte
	if _, err = io.ReadFull(c, reply[:]); err != nil || reply[1] != 0 {
		t.Fatal("mixed request", err, reply)
	}
	var ip net.IP
	switch reply[3] {
	case 1:
		ip = make([]byte, 4)
	case 4:
		ip = make([]byte, 16)
	default:
		t.Fatal("invalid mixed bind address", reply)
	}
	if _, err = io.ReadFull(c, ip); err != nil {
		t.Fatal(err)
	}
	var p [2]byte
	if _, err = io.ReadFull(c, p[:]); err != nil {
		t.Fatal(err)
	}
	if ip.IsUnspecified() {
		ip = net.IPv4(127, 0, 0, 1)
	}
	return c, net.JoinHostPort(ip.String(), strconv.Itoa(int(binary.BigEndian.Uint16(p[:]))))
}

func TestRealFiveProtocolsTCPUDP(t *testing.T) {
	binary := os.Getenv("SING_BOX_BINARY")
	if binary == "" {
		t.Skip("set SING_BOX_BINARY for real protocol checks")
	}
	root, err := os.MkdirTemp("", "sb-protocol-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	used := map[int]bool{}
	profile, err := manage.NewProfile(root, used, "")
	if err != nil {
		t.Fatal(err)
	}
	profile.VMessTLS = true
	cert, err := tls.LoadX509KeyPair(profile.Certificate, profile.Key)
	if err != nil {
		t.Fatal(err)
	}
	handshake := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "tls-target") }))
	handshake.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	handshake.StartTLS()
	t.Cleanup(handshake.Close)
	_, handshakePort, _ := net.SplitHostPort(handshake.Listener.Addr().String())
	profile.RealitySNI, profile.HandshakeHost = "www.bing.com", "127.0.0.1"
	profile.HandshakePort, _ = strconv.Atoi(handshakePort)
	ordinary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Connection", "close")
		_, _ = io.WriteString(w, "ordinary-exit")
	}))
	t.Cleanup(ordinary.Close)
	landing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Connection", "close")
		_, _ = io.WriteString(w, "vpngate-exit")
	}))
	t.Cleanup(landing.Close)
	directUDP, vpnUDP := protocolUDP(t, "direct:"), protocolUDP(t, "vpn:")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	workerSocket := filepath.Join(root, "worker.sock")
	w := worker{socket: workerSocket, dialOverride: func(ctx context.Context, network, _ string) (net.Conn, error) {
		address := landing.Listener.Addr().String()
		if network == "udp" {
			address = vpnUDP
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	go func() { _ = w.serve(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := net.DialTimeout("unix", workerSocket, 100*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("protocol worker did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	proxy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { proxy.Close() })
	cfg := defaults()
	cfg.ProxyUser, cfg.ProxyPass = "ygvg-JP", strings.Repeat("a", 64)
	cfg.DataDir = root
	m := newManager(cfg, "")
	m.ready, m.current = true, &socketTunnel{fakeTunnel: &fakeTunnel{}, path: workerSocket}
	t.Cleanup(m.revoke)
	t.Cleanup(m.sourceClient.CloseIdleConnections)
	go func() { _ = serveProxy(ctx, proxy, m) }()
	_, proxyPort, _ := net.SplitHostPort(proxy.Addr().String())
	pp, _ := strconv.Atoi(proxyPort)
	vpnProfile, err := manage.NewProfile(root, used, "JP-")
	if err != nil {
		t.Fatal(err)
	}
	vpnProfile.RealitySNI, vpnProfile.HandshakeHost, vpnProfile.HandshakePort = profile.RealitySNI, profile.HandshakeHost, profile.HandshakePort
	vpnProfile.VMessTLS = true
	// Exercise the same custom-credential path used by the menus before making
	// real TCP/UDP connections with each ordinary and gateway protocol.
	app := manage.App{Name: "protocol-test", Address: "127.0.0.1", Profile: profile, Inbounds: manage.MakeInbounds(profile, ""), SocksPort: 40000}
	if err := manage.CommitApp(ctx, root, binary, &app); err != nil {
		t.Fatal(err)
	}
	vpnProfile.Country = "JP"
	secret := vpnProfile.Secrets
	secret.SocksUser, secret.SocksPass = cfg.ProxyUser, cfg.ProxyPass
	vpnProfile.Secrets = manage.Secrets{}
	settings := manage.VPNSettings{ActiveCountry: "JP", ProxyPort: pp, Profiles: map[string]manage.Profile{"JP": vpnProfile}}
	for _, file := range []struct {
		name  string
		value any
	}{{"settings.json", settings}, {"secrets.json", map[string]manage.Secrets{"JP": secret}}} {
		if err := manage.AtomicJSON(filepath.Join(root, "vpngate", file.name), file.value); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"ws", "/普通/连接-._~"},
		{"ws", "/VPNGate/日本-._~", "--scope", "vpngate"},
		{"credentials", "all", "01234567-89ab-4cde-8f01-23456789abcd"},
		{"credentials", "all", "12345678-9abc-4def-8012-3456789abcde", "--scope", "vpngate"},
		{"credentials", "tuic", "23456789-abcd-4ef0-8123-456789abcdef", "independent TUIC @:/?# password", "--scope", "vpngate"},
	} {
		if err := manage.RunConfig(ctx, append([]string{"--root", root, "--core", binary}, args...), io.Discard); err != nil {
			t.Fatal("custom protocol credentials", err)
		}
	}
	subscriptionPort, err := manage.AvailablePort(used)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 43)
	if err := manage.AtomicJSON(filepath.Join(root, "subscription.json"), manage.Object{"enabled": true, "port": subscriptionPort, "token": token, "listen": "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	subscriptionCtx, stopSubscription := context.WithCancel(ctx)
	subscriptionDone := make(chan error, 1)
	go func() {
		subscriptionDone <- manage.RunSubscription(subscriptionCtx, []string{"--root", root}, io.Discard)
	}()
	t.Cleanup(func() {
		stopSubscription()
		select {
		case err := <-subscriptionDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("protocol subscription server did not stop")
		}
	})
	subscriptionURL := "http://127.0.0.1:" + strconv.Itoa(subscriptionPort) + "/" + token + "/"
	if _, err := manage.ReadJSON(filepath.Join(root, "app.json"), &app); err != nil {
		t.Fatal(err)
	}
	profile = app.Profile
	if _, err := manage.ReadJSON(filepath.Join(root, "vpngate", "settings.json"), &settings); err != nil {
		t.Fatal(err)
	}
	var private map[string]manage.Secrets
	if _, err := manage.ReadJSON(filepath.Join(root, "vpngate", "secrets.json"), &private); err != nil {
		t.Fatal(err)
	}
	vpnProfile = settings.Profiles["JP"]
	vpnProfile.Secrets = private["JP"]
	inbounds, vpnInbounds := manage.MakeInbounds(profile, ""), manage.MakeInbounds(vpnProfile, "vg-JP-")
	server, err := manage.ServerConfig(inbounds, nil, nil, 40000)
	if err != nil {
		t.Fatal(err)
	}
	protocolCore(t, root, "ordinary", binary, server, inbounds[0]["listen_port"].(int))
	vpnServer := manage.Object{"inbounds": vpnInbounds, "outbounds": []manage.Object{{"type": "socks", "tag": "gateway", "server": "127.0.0.1", "server_port": pp, "username": cfg.ProxyUser, "password": cfg.ProxyPass, "version": "5"}}, "route": manage.Object{"final": "gateway"}}
	protocolCore(t, root, "gateway", binary, vpnServer, vpnInbounds[0]["listen_port"].(int))
	for _, group := range []struct {
		label   string
		entries []manage.Object
	}{{"ordinary", inbounds}, {"vpngate", vpnInbounds}} {
		directory := root
		if group.label == "vpngate" {
			directory = filepath.Join(root, "vpngate")
		}
		// Read the files actually published by the management commands. Building
		// fresh nodes here would miss errors in export assembly and URI import.
		var exported struct {
			Outbounds []manage.Object `json:"outbounds"`
		}
		var mihomo struct {
			Proxies []manage.Object `json:"proxies"`
		}
		for _, file := range []struct {
			name  string
			value any
		}{{"sbox.json", &exported}, {"clmi.yaml", &mihomo}} {
			if _, err := manage.ReadJSON(filepath.Join(directory, file.name), file.value); err != nil {
				t.Fatal(err)
			}
		}
		if len(exported.Outbounds) < len(group.entries) || len(mihomo.Proxies) != len(group.entries) {
			t.Fatal("subscription is missing protocol nodes")
		}
		for index, inbound := range group.entries {
			kind := inbound["type"].(string)
			if exported.Outbounds[index]["type"] != kind || mihomo.Proxies[index]["type"] != kind || exported.Outbounds[index]["tag"] != mihomo.Proxies[index]["name"] {
				t.Fatal("subscription protocol or name mismatch", kind)
			}
			for _, format := range []string{"sing-box", "mihomo", "provider", "uri"} {
				t.Run(group.label+"/"+format+"/"+kind, func(t *testing.T) {
					mihomoBinary := os.Getenv("MIHOMO_BINARY")
					if format != "sing-box" && mihomoBinary == "" {
						t.Skip("set MIHOMO_BINARY for Mihomo and URI import checks")
					}
					// Mihomo's URI converter ignores VMess insecure and TUIC
					// allow_insecure. Their self-signed TLS requires native configs.
					if format == "uri" && (kind == "vmess" || kind == "tuic") {
						t.Skip("Mihomo URI importer cannot represent self-signed TLS for this protocol; native subscription is tested")
					}
					node := exported.Outbounds[index]
					port, err := manage.AvailablePort(used)
					if err != nil {
						t.Fatal(err)
					}
					name := group.label + "-" + format + "-" + kind
					if format == "sing-box" {
						client := manage.Object{"inbounds": []manage.Object{{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": []manage.Object{node}, "route": manage.Object{"final": node["tag"]}}
						protocolCore(t, root, name, binary, client, port)
					} else {
						client := manage.Object{"mixed-port": port, "bind-address": "127.0.0.1", "mode": "rule", "log-level": "debug", "rules": []string{"MATCH," + node["tag"].(string)}, "proxies": []manage.Object{mihomo.Proxies[index]}}
						if format == "uri" || format == "provider" {
							delete(client, "proxies")
							provider := manage.Object{"type": "file", "path": filepath.ToSlash(filepath.Join(directory, "jhsub-base64.txt"))}
							if format == "provider" {
								endpoint := subscriptionURL
								if group.label == "vpngate" {
									endpoint += "vpngate/"
								}
								provider = manage.Object{"type": "http", "url": endpoint + "proxies.yaml", "path": "./" + name + "-provider.yaml", "interval": 3600}
							}
							client["proxy-providers"] = manage.Object{"subscription": provider}
							client["proxy-groups"] = []manage.Object{{"name": "selected", "type": "select", "use": []string{"subscription"}, "filter": "^" + regexp.QuoteMeta(node["tag"].(string)) + "$"}}
							client["rules"] = []string{"MATCH,selected"}
						}
						path := filepath.Join(root, name+".json")
						if err := manage.AtomicJSON(path, client); err != nil {
							t.Fatal(err)
						}
						protocolProcess(t, root, name, exec.Command(mihomoBinary, "-d", root, "-f", path), port)
						protocolMihomoReady(t, port, ordinary.Listener.Addr().String())
					}
					c, _ := protocolMixed(t, port, 1, ordinary.Listener.Addr().String())
					_, _ = io.WriteString(c, "GET / HTTP/1.0\r\nHost: localhost\r\n\r\n")
					response, err := http.ReadResponse(bufio.NewReader(c), nil)
					if err != nil {
						t.Fatal("TCP HTTP response", err)
					}
					data, err := io.ReadAll(response.Body)
					response.Body.Close()
					c.Close()
					expected := "ordinary-exit"
					if group.label == "vpngate" {
						expected = "vpngate-exit"
					}
					if err != nil || !bytes.Contains(data, []byte(expected)) {
						t.Fatal("TCP reached wrong exit", err, string(data))
					}
					control, bind := protocolMixed(t, port, 3, directUDP)
					defer control.Close()
					udp, err := net.DialTimeout("udp4", bind, 3*time.Second)
					if err != nil {
						t.Fatal(err)
					}
					defer udp.Close()
					_ = udp.SetDeadline(time.Now().Add(15 * time.Second))
					header, err := udpResponseHeader(directUDP)
					if err != nil {
						t.Fatal(err)
					}
					_, err = udp.Write(append(header, []byte("udp-check")...))
					if err != nil {
						t.Fatal(err)
					}
					reply := make([]byte, 1024)
					n, err := udp.Read(reply)
					expected = "direct:udp-check"
					if group.label == "vpngate" {
						expected = "vpn:udp-check"
					}
					if err != nil || !bytes.HasSuffix(reply[:n], []byte(expected)) {
						t.Fatal("UDP reached wrong exit", err, string(reply[:n]))
					}
					t.Logf("%s %s %s: TCP and UDP passed", group.label, format, kind)
				})
			}
		}
	}
}
