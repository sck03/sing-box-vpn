package manage

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestSubscriptionHTTPSListener(t *testing.T) {
	root := t.TempDir()
	cert, key := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
	if err := EnsureCertificate(cert, key, "sub.example.test"); err != nil {
		t.Fatal(err)
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	settings := subscriptionSettings{Enabled: true, Listen: "127.0.0.1", Port: port, PublicBaseURL: "https://sub.example.test:" + strconv.Itoa(port), Certificate: cert, Key: key}
	if err = AtomicJSON(filepath.Join(root, "subscription.json"), settings); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunSubscription(ctx, []string{"--root", root}, io.Discard) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("listener failed to stop")
		}
	})
	raw, err := os.ReadFile(cert)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(raw)
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "sub.example.test", MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	endpoint := "https://127.0.0.1:" + strconv.Itoa(port) + "/user"
	deadline := time.Now().Add(5 * time.Second)
	for {
		req, _ := http.NewRequest("GET", endpoint, nil)
		req.Host = "sub.example.test:" + strconv.Itoa(port)
		res, err := client.Do(req)
		if err == nil {
			res.Body.Close()
			if res.StatusCode != 200 {
				t.Fatal(res.Status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	req, _ := http.NewRequest("GET", endpoint, nil)
	req.Host = "wrong.example.test"
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatal("wrong Host accepted", res.Status)
	}
	if res, err = http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/user"); err == nil {
		res.Body.Close()
		if res.StatusCode < 400 {
			t.Fatal("plaintext served")
		}
	}
}
func TestSubscriptionCertificateRelocation(t *testing.T) {
	root := t.TempDir()
	cert, key := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
	if err := EnsureCertificate(cert, key, "sub.example.test"); err != nil {
		t.Fatal(err)
	}
	c := PlatformConfig{Listener: subscriptionSettings{Enabled: true, Certificate: cert, Key: key}}
	if !certificateUses(c, cert, "sb-subscription") {
		t.Fatal("renewal omitted subscription")
	}
	b := platformBackup{Root: root, Files: map[string]string{"cert.pem": cert, "key.pem": key}}
	dest := t.TempDir()
	if err := relocateBackup(&c, b, dest); err != nil {
		t.Fatal(err)
	}
	if c.Listener.Certificate != filepath.ToSlash(filepath.Join(dest, "cert.pem")) || len(certificatePaths(c)) != 2 {
		t.Fatal(c.Listener, certificatePaths(c))
	}
}

func TestRestorePreviewUsesArchivedListenerCertificate(t *testing.T) {
	root, core, _ := appFixture(t)
	s, config, _ := platformFixture(t, root, core)
	cert, key := filepath.Join(root, "sub-cert.pem"), filepath.Join(root, "sub-key.pem")
	if err := EnsureCertificate(cert, key, "sub.example.test"); err != nil {
		t.Fatal(err)
	}
	config.Listener = subscriptionSettings{Enabled: true, Listen: "127.0.0.1", Port: 2097, PublicBaseURL: "https://sub.example.test:2097", Certificate: cert, Key: key}
	id, err := s.Store.Queue(t.Context(), "apply", "test", []byte(`{}`), 1, mustJSON(t, config))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Applied(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Finish(t.Context(), id, nil); err != nil {
		t.Fatal(err)
	}
	password := []byte("restore-preview-password")
	backup, err := s.Backup(t.Context(), password)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{cert, key} {
		if err = os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("backup", "snapshot.sbvpn")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(backup); err != nil {
		t.Fatal(err)
	}
	if err = writer.WriteField("password", string(password)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/restore", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	s.servePlatformBackup(recorder, req, "test")
	if recorder.Code != 200 {
		t.Fatal(recorder.Code, recorder.Body.String())
	}
	if _, err = os.Stat(cert); !os.IsNotExist(err) {
		t.Fatal("preview rewrote live certificate", err)
	}
}
