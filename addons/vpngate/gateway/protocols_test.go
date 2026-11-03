package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	logPath := filepath.Join(root, name+".log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "-c", path)
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
		profile manage.Profile
		entries []manage.Object
	}{{"ordinary", profile, inbounds}, {"vpngate", vpnProfile, vpnInbounds}} {
		for _, inbound := range group.entries {
			kind := inbound["type"].(string)
			t.Run(group.label+"-"+kind, func(t *testing.T) {
				node, err := manage.Outbound(inbound, group.profile, "127.0.0.1", group.label+"-"+kind)
				if err != nil {
					t.Fatal(err)
				}
				port, err := manage.AvailablePort(used)
				if err != nil {
					t.Fatal(err)
				}
				client := manage.Object{"inbounds": []manage.Object{{"type": "mixed", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": []manage.Object{node}, "route": manage.Object{"final": node["tag"]}}
				protocolCore(t, root, group.label+"-"+kind, binary, client, port)
				c, _ := protocolMixed(t, port, 1, ordinary.Listener.Addr().String())
				_, _ = io.WriteString(c, "GET / HTTP/1.0\r\nHost: localhost\r\n\r\n")
				data, err := io.ReadAll(c)
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
				t.Log(fmt.Sprintf("%s %s: TCP and UDP passed", group.label, kind))
			})
		}
	}
}
