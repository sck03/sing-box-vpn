//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/manage"
)

const testServerNS = "ygtest-server"
const testHostVeth = "ygtest-host"
const testPeerVeth = "ygtest-peer"
const testRemoteIP = "203.0.113.2"
const testTargetIP = "198.51.100.10"
const testExitIP = "198.51.100.99"
const testHostExitIP = "198.51.100.88"

func linuxCommand(t *testing.T, command string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, command, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v: %s", command, args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func linuxProcess(t *testing.T, root, name string, environment []string, command string, args ...string) func() {
	t.Helper()
	log, err := os.Create(filepath.Join(root, name+".log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(command, args...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.Env = append(os.Environ(), environment...)
	if err = cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	stop := func() {
		select {
		case <-done:
		default:
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
		}
		log.Close()
	}
	t.Cleanup(stop)
	return stop
}
func linuxWait(t *testing.T, duration time.Duration, condition func() bool) {
	t.Helper()
	until := time.Now().Add(duration)
	for {
		if condition() {
			return
		}
		if time.Now().After(until) {
			t.Fatal("Linux fixture condition timed out")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func linuxCertificates(t *testing.T, root string) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "yg-test-ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	if err = manage.AtomicBytes(filepath.Join(root, "ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600); err != nil {
		t.Fatal(err)
	}
	for i, role := range []string{"server", "client"} {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		certificate := &x509.Certificate{SerialNumber: big.NewInt(int64(i + 2)), Subject: pkix.Name{CommonName: role}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		if role == "server" {
			certificate.Subject.CommonName, certificate.DNSNames, certificate.ExtKeyUsage = "exit.test", []string{"exit.test"}, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		}
		der, err := x509.CreateCertificate(rand.Reader, certificate, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		private, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		if err = manage.AtomicBytes(filepath.Join(root, role+".crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
		if err = manage.AtomicBytes(filepath.Join(root, role+".key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// This helper runs as a child test process inside the controlled server netns.
func TestNamespaceDataServer(t *testing.T) {
	root := os.Getenv("YGVG_TEST_DATA_DIR")
	if root == "" {
		t.Skip("network namespace fixture helper")
	}
	cert, err := tls.LoadX509KeyPair(filepath.Join(root, "server.crt"), filepath.Join(root, "server.key"))
	if err != nil {
		t.Fatal(err)
	}
	https := &http.Server{Addr: testTargetIP + ":8443", TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exit := testHostExitIP
		if strings.HasPrefix(r.RemoteAddr, "10.8.") {
			exit = testExitIP
		}
		_, _ = io.WriteString(w, exit)
	})}
	go func() {
		if err := https.ListenAndServeTLS("", ""); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()
	tcp, err := net.Listen("tcp4", testTargetIP+":9000")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	go func() {
		for {
			c, err := tcp.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	udp, err := net.ListenPacket("udp4", testTargetIP+":9001")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	go func() {
		buf := make([]byte, maxPacket)
		for {
			n, addr, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = udp.WriteTo(buf[:n], addr)
		}
	}()
	dns, err := net.ListenPacket("udp4", "1.1.1.1:53")
	if err != nil {
		t.Fatal(err)
	}
	defer dns.Close()
	buf := make([]byte, 4096)
	for {
		n, addr, err := dns.ReadFrom(buf)
		if err != nil {
			return
		}
		packet := buf[:n]
		if len(packet) < 17 {
			continue
		}
		end := 12
		for end < len(packet) && packet[end] != 0 {
			end += int(packet[end]) + 1
		}
		end++
		if end+4 > len(packet) {
			continue
		}
		kind := binary.BigEndian.Uint16(packet[end : end+2])
		count := uint16(0)
		if kind == 1 {
			count = 1
		}
		response := append([]byte(nil), packet[:2]...)
		for _, value := range []uint16{0x8180, 1, count, 0, 0} {
			response = binary.BigEndian.AppendUint16(response, value)
		}
		response = append(response, packet[12:end+4]...)
		if kind == 1 {
			response = append(response, 0xc0, 0x0c)
			for _, value := range []uint16{1, 1} {
				response = binary.BigEndian.AppendUint16(response, value)
			}
			response = binary.BigEndian.AppendUint32(response, 60)
			response = binary.BigEndian.AppendUint16(response, 4)
			response = append(response, net.ParseIP(testTargetIP).To4()...)
		}
		_, _ = dns.WriteTo(response, addr)
		log, err := os.OpenFile(filepath.Join(root, "dns-sources.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err == nil {
			_, _ = fmt.Fprintln(log, addr.(*net.UDPAddr).IP.String())
			log.Close()
		}
	}
}

func TestLinuxNamespaceIsolation(t *testing.T) {
	if os.Getenv("RUN_NETNS_TESTS") != "1" {
		t.Skip("set RUN_NETNS_TESTS=1 for privileged offline acceptance")
	}
	if os.Geteuid() != 0 {
		t.Fatal("namespace acceptance requires root")
	}
	gateway := os.Getenv("GATEWAY_BINARY")
	if gateway == "" {
		t.Fatal("set GATEWAY_BINARY")
	}
	if list := linuxCommand(t, "ip", "netns", "list"); strings.Contains(list, namespace) || strings.Contains(list, testServerNS) {
		t.Fatal("test namespaces already exist")
	}
	root := t.TempDir()
	hosts, err := os.ReadFile("/etc/hosts")
	if err != nil {
		t.Fatal(err)
	}
	forward := linuxCommand(t, "sysctl", "-n", "net.ipv4.ip_forward")
	originalRoute := linuxCommand(t, "ip", "-4", "route", "show", "default")
	configPath := filepath.Join(root, "gateway.json")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, gateway, "cleanup", "--config", configPath).Run()
		_ = exec.CommandContext(ctx, "ip", "netns", "del", testServerNS).Run()
		_ = exec.CommandContext(ctx, "ip", "link", "del", testHostVeth).Run()
		_ = os.WriteFile("/etc/hosts", hosts, 0644)
		_ = exec.CommandContext(ctx, "sysctl", "-qw", "net.ipv4.ip_forward="+forward).Run()
	})
	t.Cleanup(func() {
		if t.Failed() {
			for _, name := range []string{"openvpn-server.log", "data-server.log", "daemon.log", "crash-restart.log", "restart.log", "data/state.json", "data/openvpn.log"} {
				if data, err := os.ReadFile(filepath.Join(root, name)); err == nil {
					if len(data) > 12000 {
						data = data[len(data)-12000:]
					}
					t.Logf("%s: %s", name, data)
				}
			}
		}
	})
	linuxCommand(t, "ip", "netns", "add", testServerNS)
	linuxCommand(t, "ip", "link", "add", testHostVeth, "type", "veth", "peer", "name", testPeerVeth)
	linuxCommand(t, "ip", "link", "set", testPeerVeth, "netns", testServerNS)
	linuxCommand(t, "ip", "addr", "add", "203.0.113.1/30", "dev", testHostVeth)
	linuxCommand(t, "ip", "link", "set", testHostVeth, "up")
	for _, args := range [][]string{{"link", "set", "lo", "up"}, {"addr", "add", testRemoteIP + "/30", "dev", testPeerVeth}, {"link", "set", testPeerVeth, "up"}, {"addr", "add", testTargetIP + "/32", "dev", "lo"}, {"addr", "add", "1.1.1.1/32", "dev", "lo"}} {
		linuxCommand(t, "ip", append([]string{"netns", "exec", testServerNS, "ip"}, args...)...)
	}
	linuxCommand(t, "ip", "route", "add", testTargetIP+"/32", "via", testRemoteIP, "dev", testHostVeth)
	if err = os.WriteFile("/etc/hosts", append(append([]byte(nil), hosts...), []byte("\n"+testTargetIP+" exit.test\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	linuxCertificates(t, root)
	serverConfig := strings.Join([]string{"port 1194", "proto udp", "local " + testRemoteIP, "dev tun0", "server 10.8.0.0 255.255.255.0", "topology subnet", "dh none", "ca " + filepath.Join(root, "ca.crt"), "cert " + filepath.Join(root, "server.crt"), "key " + filepath.Join(root, "server.key"), "tls-version-min 1.2", "data-ciphers AES-256-GCM:AES-128-GCM", "auth SHA256", `push "redirect-gateway def1"`, "keepalive 5 20", "verb 3"}, "\n") + "\n"
	if err = os.WriteFile(filepath.Join(root, "server.conf"), []byte(serverConfig), 0600); err != nil {
		t.Fatal(err)
	}
	linuxProcess(t, root, "openvpn-server", nil, "ip", "netns", "exec", testServerNS, "openvpn", "--config", filepath.Join(root, "server.conf"))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	linuxProcess(t, root, "data-server", []string{"YGVG_TEST_DATA_DIR=" + root}, "ip", "netns", "exec", testServerNS, executable, "-test.run=^TestNamespaceDataServer$")
	profile := "client\ndev tun\nproto udp\nremote " + testRemoteIP + " 1194\nremote-cert-tls server\nauth SHA256\n"
	for _, block := range []struct{ kind, file string }{{"ca", "ca.crt"}, {"cert", "client.crt"}, {"key", "client.key"}} {
		data, err := os.ReadFile(filepath.Join(root, block.file))
		if err != nil {
			t.Fatal(err)
		}
		profile += "<" + block.kind + ">\n" + string(data) + "</" + block.kind + ">\n"
	}
	var csvData bytes.Buffer
	csvWriter := csv.NewWriter(&csvData)
	_ = csvWriter.Write([]string{"HostName", "IP", "CountryShort", "Ping", "Speed", "OpenVPN_ConfigData_Base64"})
	_ = csvWriter.Write([]string{"volunteer-test", testRemoteIP, "JP", "1", "10000", base64.StdEncoding.EncodeToString([]byte(profile))})
	csvWriter.Flush()
	var country atomic.Value
	country.Store("JP")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/nodes") {
			_, _ = w.Write(csvData.Bytes())
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"location": map[string]any{"country_code": country.Load()}, "asn": map[string]any{"asn": 42, "type": "isp", "org": "test-isp"}, "is_datacenter": false})
		}
	}))
	t.Cleanup(provider.Close)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cfg := defaults()
	cfg.Country, cfg.Enabled, cfg.Listen, cfg.ProxyUser, cfg.ProxyPass = "JP", true, "127.0.0.1:"+strconv.Itoa(port), "ygvg-JP", strings.Repeat("a", 64)
	cfg.DataDir, cfg.RunDir = filepath.Join(root, "data"), filepath.Join(root, "run")
	cfg.Sources, cfg.GeoURL, cfg.ProbeURL, cfg.HealthInterval, cfg.ConnectTimeout = []string{provider.URL + "/nodes"}, provider.URL+"/geo?q=%s", "https://exit.test:8443", 2, 30
	if err = atomicJSON(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	environment := []string{"SSL_CERT_FILE=" + filepath.Join(root, "ca.crt")}
	stopDaemon := linuxProcess(t, root, "daemon", environment, gateway, "daemon", "--config", configPath)
	tryCTL := func(op string) (Status, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var status Status
		data, err := exec.CommandContext(ctx, gateway, "ctl", op, "--config", configPath).Output()
		if err != nil {
			return status, err
		}
		err = json.Unmarshal(data, &status)
		return status, err
	}
	ctl := func(op string) Status {
		status, err := tryCTL(op)
		if err != nil {
			t.Fatal("control operation", op, err)
		}
		return status
	}
	linuxWait(t, 90*time.Second, func() bool { status, err := tryCTL("status"); return err == nil && status.State == "ready" })
	if linuxCommand(t, "ip", "-4", "route", "show", "default") != originalRoute {
		t.Fatal("host default route changed")
	}
	// Simulate a daemon crash that bypasses normal teardown. Its OpenVPN and
	// worker children must be reclaimed before the next daemon builds a tunnel.
	orphanNamespaces := map[string]string{}
	for _, pid := range strings.Fields(linuxCommand(t, "ip", "netns", "pids", namespace)) {
		id, err := os.Readlink(filepath.Join("/proc", pid, "ns/net"))
		if err != nil {
			t.Fatal(err)
		}
		orphanNamespaces[pid] = id
	}
	if len(orphanNamespaces) < 2 {
		t.Fatal("fixture did not start OpenVPN and a namespace worker")
	}
	daemonPID, err := os.ReadFile(filepath.Join(cfg.DataDir, "daemon.lock"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(daemonPID)))
	if err != nil || pid <= 1 || pid == os.Getpid() {
		t.Fatal("invalid fixture daemon PID", err)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	stopDaemon()
	if linuxCommand(t, "ip", "netns", "pids", namespace) == "" {
		t.Fatal("crash fixture did not leave namespace children to recover")
	}
	stopDaemon = linuxProcess(t, root, "crash-restart", environment, gateway, "daemon", "--config", configPath)
	linuxWait(t, 90*time.Second, func() bool { status, err := tryCTL("status"); return err == nil && status.State == "ready" })
	for pid, oldNamespace := range orphanNamespaces {
		if current, err := os.Readlink(filepath.Join("/proc", pid, "ns/net")); err == nil && current == oldNamespace {
			t.Fatal("daemon restart retained an orphaned namespace process", pid)
		}
	}
	if linuxCommand(t, "ip", "-4", "route", "show", "default") != originalRoute {
		t.Fatal("crash recovery changed the host default route")
	}
	connect := func(command byte, target string, domain bool) (net.Conn, string) {
		c, err := net.DialTimeout("tcp4", "127.0.0.1:"+strconv.Itoa(port), 3*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = c.Write([]byte{5, 1, 2})
		var reply [2]byte
		if _, err = io.ReadFull(c, reply[:]); err != nil || reply != [2]byte{5, 2} {
			t.Fatal("fixture auth method", err)
		}
		user, pass := cfg.ProxyUser, cfg.ProxyPass
		auth := append([]byte{1, byte(len(user))}, []byte(user)...)
		auth = append(auth, byte(len(pass)))
		auth = append(auth, []byte(pass)...)
		_, _ = c.Write(auth)
		if _, err = io.ReadFull(c, reply[:]); err != nil || reply[1] != 0 {
			t.Fatal("fixture auth failed", err)
		}
		request := requestAddress(command, target)
		if domain {
			host, p, _ := net.SplitHostPort(target)
			n, _ := strconv.Atoi(p)
			request = append([]byte{5, command, 0, 3, byte(len(host))}, []byte(host)...)
			request = binary.BigEndian.AppendUint16(request, uint16(n))
		}
		_, _ = c.Write(request)
		var bind [10]byte
		if _, err = io.ReadFull(c, bind[:]); err != nil || bind[1] != 0 {
			t.Fatal("fixture connect refused", err, bind)
		}
		return c, net.JoinHostPort(net.IP(bind[4:8]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(bind[8:]))))
	}
	c, _ := connect(1, testTargetIP+":9000", false)
	_, _ = io.WriteString(c, "tcp-via-openvpn")
	tcpReply := make([]byte, 15)
	if _, err = io.ReadFull(c, tcpReply); err != nil || string(tcpReply) != "tcp-via-openvpn" {
		t.Fatal("TCP did not traverse OpenVPN", err)
	}
	control, bind := connect(3, "0.0.0.0:0", false)
	udp, err := net.Dial("udp4", bind)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	_ = udp.SetDeadline(time.Now().Add(3 * time.Second))
	header, _ := udpResponseHeader(testTargetIP + ":9001")
	packet := append(header, []byte("udp-via-openvpn")...)
	_, _ = udp.Write(packet)
	answer := make([]byte, 1024)
	n, err := udp.Read(answer)
	if err != nil || !bytes.HasSuffix(answer[:n], []byte("udp-via-openvpn")) {
		t.Fatal("UDP did not traverse OpenVPN", err)
	}
	byName, _ := connect(1, "business.test:9000", true)
	_, _ = io.WriteString(byName, "dns-via-tun")
	dnsReply := make([]byte, 11)
	if _, err = io.ReadFull(byName, dnsReply); err != nil || string(dnsReply) != "dns-via-tun" {
		t.Fatal("tunnel DNS failed", err)
	}
	byName.Close()
	sources, err := os.ReadFile(filepath.Join(root, "dns-sources.log"))
	if err != nil || len(sources) == 0 {
		t.Fatal("DNS query was not observed")
	}
	for _, source := range strings.Fields(string(sources)) {
		if !strings.HasPrefix(source, "10.8.") {
			t.Fatal("DNS leaked outside tunnel", source)
		}
	}
	killed := false
	for _, pidText := range strings.Fields(linuxCommand(t, "ip", "netns", "pids", namespace)) {
		comm, _ := os.ReadFile(filepath.Join("/proc", pidText, "comm"))
		if strings.TrimSpace(string(comm)) == "openvpn" {
			pid, _ := strconv.Atoi(pidText)
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
				t.Fatal(err)
			}
			killed = true
			break
		}
	}
	if !killed {
		t.Fatal("OpenVPN process was not found")
	}
	_ = udp.SetDeadline(time.Now().Add(500 * time.Millisecond))
	_, _ = udp.Write(packet)
	if _, err := udp.Read(answer); err == nil {
		t.Fatal("existing UDP session survived tunnel death")
	}
	_, _ = io.WriteString(c, "must-not-leak")
	_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if n, _ := c.Read(answer); n > 0 && string(answer[:n]) == "must-not-leak" {
		t.Fatal("existing TCP session leaked")
	}
	linuxWait(t, 10*time.Second, func() bool { status, err := tryCTL("status"); return err == nil && status.State != "ready" })
	fresh, err := net.DialTimeout("tcp4", "127.0.0.1:"+strconv.Itoa(port), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = fresh.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = fresh.Write([]byte{5, 1, 2})
	var authReply [2]byte
	if _, err = io.ReadFull(fresh, authReply[:]); err != nil {
		t.Fatal(err)
	}
	credentials := append([]byte{1, byte(len(cfg.ProxyUser))}, []byte(cfg.ProxyUser)...)
	credentials = append(credentials, byte(len(cfg.ProxyPass)))
	credentials = append(credentials, []byte(cfg.ProxyPass)...)
	_, _ = fresh.Write(credentials)
	if _, err = io.ReadFull(fresh, authReply[:]); err != nil {
		t.Fatal(err)
	}
	_, _ = fresh.Write(requestAddress(1, testTargetIP+":9000"))
	var denied [10]byte
	if _, err = io.ReadFull(fresh, denied[:]); err == nil && denied[1] == 0 {
		t.Fatal("new connection bypassed dead tunnel")
	}
	fresh.Close()
	c.Close()
	control.Close()
	_ = ctl("stop")
	stopDaemon()
	stopRestart := linuxProcess(t, root, "restart", environment, gateway, "daemon", "--config", configPath)
	linuxWait(t, 10*time.Second, func() bool { status, err := tryCTL("status"); return err == nil && status.State == "stopped" })
	if strings.Contains(linuxCommand(t, "ip", "netns", "list"), namespace) {
		t.Fatal("disabled daemon recreated tunnel")
	}
	country.Store("KR")
	_ = ctl("start")
	var lastStatus Status
	defer func() {
		if t.Failed() {
			t.Logf("last wrong-country startup status before cleanup: %+v", lastStatus)
		}
	}()
	linuxWait(t, 90*time.Second, func() bool {
		status, err := tryCTL("status")
		if err == nil {
			lastStatus = status
		}
		return err == nil && strings.Contains(status.Reason, "does not match JP")
	})
	if ctl("status").State == "ready" {
		t.Fatal("wrong-country tunnel enabled")
	}
	_ = ctl("stop")
	stopRestart()
}
