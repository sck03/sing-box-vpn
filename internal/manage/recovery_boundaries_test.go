package manage

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"github.com/sck03/sing-box-vpn/internal/publicaccess"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestExpiredPublicCertificatesDoNotBlockWorkerRecovery(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, _ := platformFixture(t, root, core)
	certPath, keyPath := filepath.Join(root, "panel-cert.pem"), filepath.Join(root, "panel-key.pem")
	if err := EnsureCertificate(certPath, keyPath, "panel.example.test"); err != nil {
		t.Fatal(err)
	}
	valid := readBytes(t, certPath)
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	leaf.NotBefore, leaf.NotAfter = time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, leaf.PublicKey, pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err = AtomicBytes(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	options := PanelOptions{Listen: "127.0.0.1:0", PublicURL: "https://panel.example.test:29443", Certificate: certPath, Key: keyPath}
	if err = options.validate(true); err == nil {
		t.Fatal("expired candidate accepted")
	}
	if _, err = publicaccess.Options(options).TLS().GetCertificate(nil); err == nil {
		t.Fatal("expired certificate served")
	}
	// The download listener used to prevent projection recovery even when the
	// panel itself was accessed through a reverse proxy.
	config.Listener = subscriptionSettings{Enabled: true, Listen: "127.0.0.1", Port: 2097, PublicBaseURL: "https://panel.example.test:2097", Certificate: certPath, Key: keyPath}
	if err = config.Validate(); err == nil {
		t.Fatal("expired listener candidate accepted")
	}
	id, err := service.Store.Queue(t.Context(), "apply", "test", []byte("{}"), 1, mustJSON(t, config))
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Store.Applied(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	if err = service.Store.Finish(t.Context(), id, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- service.Serve(ctx, options, io.Discard) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("worker did not stop")
		}
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for !service.workerReady.Load() {
		select {
		case <-poll.C:
		case <-deadline.C:
			t.Fatal("expired certificate prevented worker recovery")
		}
	}
	if err = AtomicBytes(certPath, valid, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = publicaccess.Options(options).TLS().GetCertificate(nil); err != nil {
		t.Fatal("renewed material unavailable", err)
	}
}

func TestCertificateStatusAndCommandsUseAppliedRegistry(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, _ := platformFixture(t, root, core)
	config.Certificates = []CertificateAsset{{ID: "live", Provider: "acme", Domain: "live.example.test"}}
	id, err := service.Store.Queue(t.Context(), "apply", "test", []byte("{}"), 1, mustJSON(t, config))
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Store.Applied(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	if err = service.Store.Finish(t.Context(), id, nil); err != nil {
		t.Fatal(err)
	}
	config.Certificates = []CertificateAsset{{ID: "pending", Provider: "acme", Domain: "pending.example.test"}}
	id, err = service.Store.Queue(t.Context(), "apply", "test", []byte("{}"), 2, mustJSON(t, config))
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Store.Finish(t.Context(), id, errors.New("failed apply")); err != nil {
		t.Fatal(err)
	}
	status, err := service.CertificateStatus(t.Context())
	if err != nil || len(status) != 2 {
		t.Fatal(status, err)
	}
	if !status[0].Applied || status[0].Saved || !status[0].Pending || status[1].Applied || !status[1].Saved || status[1].CanRenew {
		t.Fatal(status)
	}
	for _, id := range []string{"live", "pending"} {
		if _, err = service.PrepareCertificate(t.Context(), CertificatePreparation{ID: id, Domain: "replacement.example.test", Email: "admin@example.test"}); err == nil {
			t.Fatalf("reused saved or applied certificate ID %q", id)
		}
	}
	if _, err = service.PrepareCertificate(t.Context(), CertificatePreparation{ID: "replacement", Domain: "replacement.example.test", Email: "admin@example.test"}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Queue(t.Context(), application.TaskCommand{Operation: "certificate-renew", ID: "pending"}, "test"); err == nil {
		t.Fatal("queued an unapplied certificate")
	}
	if _, err = service.Queue(t.Context(), application.TaskCommand{Operation: "certificate-renew", ID: "live"}, "test"); err != nil {
		t.Fatal(err)
	}
}

func TestUDPForwardingBelongsToSpecificInbound(t *testing.T) {
	app := App{Nodes: []ManagedNode{
		{ID: "hy-first", Protocol: "hysteria2", Port: 20001, UDPPorts: []string{"31001:31002"}},
		{ID: "hy-second", Protocol: "hysteria2", Port: 20002, UDPPorts: []string{"32001:32002"}},
	}}
	_, initial, err := firewallRules(app, VPNSettings{}, nil, subscriptionSettings{})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"--dport 31001:31002 -j REDIRECT --to-ports 20001", "--dport 32001:32002 -j REDIRECT --to-ports 20002"} {
		if !strings.Contains(initial, target) {
			t.Fatal(initial)
		}
	}
	app.Nodes[0], app.Nodes[1] = app.Nodes[1], app.Nodes[0]
	_, reordered, err := firewallRules(app, VPNSettings{}, nil, subscriptionSettings{})
	if err != nil || !strings.Contains(reordered, "--dport 32001:32002 -j REDIRECT --to-ports 20002") {
		t.Fatal(reordered, err)
	}
	app.Nodes = app.Nodes[:1]
	_, remaining, err := firewallRules(app, VPNSettings{}, nil, subscriptionSettings{})
	if err != nil || strings.Contains(remaining, "31001") || !strings.Contains(remaining, "--to-ports 20002") {
		t.Fatal(remaining, err)
	}
	app.Nodes = append(app.Nodes, ManagedNode{ID: "conflict", Protocol: "hysteria2", Port: 20003, UDPPorts: []string{"32002"}})
	if _, _, err = firewallRules(app, VPNSettings{}, nil, subscriptionSettings{}); err == nil {
		t.Fatal("overlapping ranges accepted")
	}
	app.Nodes[1].UDPPorts = []string{"20002"}
	if _, _, err = firewallRules(app, VPNSettings{}, nil, subscriptionSettings{}); err == nil {
		t.Fatal("listener overlap accepted")
	}
	raw := mustJSON(t, App{Nodes: app.Nodes[:1]})
	raw = bytes.Replace(raw, []byte(`"nodes":`), []byte(`"hopping":["31000"],"nodes":`), 1)
	var old App
	if err = strictSubscriptionJSON(raw, &old); err == nil {
		t.Fatal("removed global field accepted")
	}
}

func TestDeliveryCheckpointDoesNotReadInputCache(t *testing.T) {
	root, core, _ := appFixture(t)
	config, runtime, err := loadSubscriptionState(root)
	if err != nil {
		t.Fatal(err)
	}
	destination := sub.Delivery{ID: "device", Output: "normal-provider", Kind: "http-put", URL: "https://example.test/upload"}
	config.Deliveries = []sub.Delivery{destination}
	files, _, _, err := compilePreparedSubscriptions(t.Context(), root, core, config, runtime, nil, sub.Available)
	if err != nil {
		t.Fatal(err)
	}
	if err = publish(root, files); err != nil {
		t.Fatal(err)
	}
	// Acknowledgements depend on their small state files, not cached node bodies.
	cachePath := filepath.Join(root, subscriptionsRuntime)
	if err = AtomicBytes(cachePath, []byte("unreadable input cache"), 0600); err != nil {
		t.Fatal(err)
	}
	results := []deliveryResult{{DeliveryOutcome: application.DeliveryOutcome{ID: "device", Status: "delivered"}, State: sub.DeliveryState{ConfigHash: sub.DeliveryHash(destination), Digest: "ack", Status: "delivered", Checked: time.Now()}}}
	if _, err = checkpointDeliveries(t.Context(), root, results); err != nil || !results[0].Persisted {
		t.Fatal(results, err)
	}
	if string(readBytes(t, cachePath)) != "unreadable input cache" {
		t.Fatal("delivery rewrote input cache")
	}
	operations, err := loadSubscriptionOperations(root)
	if err != nil || operations.Deliveries["device"].Digest != "ack" {
		t.Fatal(operations, err)
	}
}

func TestAccountExportKeepsEachInboundUDPPorts(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, _ := platformFixture(t, root, core)
	var base ManagedNode
	for _, node := range config.App.Nodes {
		if node.Protocol == "hysteria2" {
			base = node
		}
	}
	first, second := base, base
	first.ID, first.Port, first.UDPPorts = "hy-first", 20001, []string{"31001:31002"}
	second.ID, second.Port, second.UDPPorts = "hy-second", 20002, []string{"32001:32002"}
	config.App.Nodes = []ManagedNode{second, first}
	if err := service.Accounts.SaveAccount(t.Context(), domain.Account{ID: "ports", Name: "Ports"}); err != nil {
		t.Fatal(err)
	}
	for _, node := range config.App.Nodes {
		credential := domain.Credential{ID: node.ID, AccountID: "ports", RuntimeID: normalScope, InboundID: node.ID, Protocol: "hysteria2", Generation: 1, Password: "independent-" + node.ID}
		if err := service.Accounts.BindBatch(t.Context(), []domain.Credential{credential}); err != nil {
			t.Fatal(err)
		}
	}
	usage, err := service.Accounts.DownloadAccount(t.Context(), "ports", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := accountNodes(t.Context(), service.Accounts, config, usage, normalScope, 0)
	if err != nil || len(nodes) != 2 {
		t.Fatal(nodes, err)
	}
	expected := map[int]string{20001: "31001:31002", 20002: "32001:32002"}
	for _, node := range nodes {
		if len(node.Ports) != 1 || node.Ports[0] != expected[node.Port] {
			t.Fatal(node)
		}
	}
}
