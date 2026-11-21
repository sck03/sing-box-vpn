package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestDeliveryCancellationPreservesAcknowledgements(t *testing.T) {
	root, _, _ := appFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var uploads atomic.Int32
	var first atomic.Bool
	first.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/slow" {
			// Wait until the fast worker starts a third request: its acknowledgement
			// has then been consumed, making cancellation deterministic.
			<-r.Context().Done()
			return
		}
		if r.URL.Path == "/cancel" && first.Swap(false) {
			cancel()
			return
		}
		if r.URL.Path == "/fast" {
			uploads.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	config, _, err := loadSubscriptionState(root)
	if err != nil {
		t.Fatal(err)
	}
	config.Deliveries = []sub.Delivery{}
	for _, id := range []string{"fast", "slow", "cancel"} {
		config.Deliveries = append(config.Deliveries, sub.Delivery{ID: id, Output: "normal-provider", Kind: "http-put", URL: server.URL + "/" + id, Automatic: true})
	}
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), config); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = deliverSubscriptions(ctx, root, "", true, &out)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var report application.DeliveryResult
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err, out.String())
	}
	state, err := loadSubscriptionRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	if state.Deliveries["fast"].Digest == "" || state.Deliveries["fast"].Success.IsZero() {
		t.Fatal("acknowledgement lost", state.Deliveries)
	}
	if report.Outcome != "partial" || !report.Interrupted {
		t.Fatal(report)
	}
	uncertain := 0
	for _, item := range report.Items {
		if item.Status == "uncertain" {
			uncertain++
		}
	}
	if uncertain == 0 {
		t.Fatal("cancelled remote attempt was reported as a definite failure", report)
	}
	if err := deliverSubscriptions(t.Context(), root, "fast", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if uploads.Load() != 1 {
		t.Fatal("acknowledged destination resent")
	}
}

func TestDeliveryCheckpointSurvivesUnrelatedApply(t *testing.T) {
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
	if err := publish(root, files); err != nil {
		t.Fatal(err)
	}
	state := sub.DeliveryState{ConfigHash: sub.DeliveryHash(destination), Digest: "ack", Status: "delivered", Checked: time.Now()}
	results := []deliveryResult{{DeliveryOutcome: application.DeliveryOutcome{ID: "device", Status: "delivered"}, State: state}}
	config.Revision++
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), config); err != nil {
		t.Fatal(err)
	}
	if _, err := checkpointDeliveries(t.Context(), root, results); err != nil || !results[0].Persisted {
		t.Fatal(err, results)
	}
	// An already compiled platform candidate must merge the acknowledgement.
	if err := files.json(subscriptionsConfig, config); err != nil {
		t.Fatal(err)
	}
	if err := publishPlatformFiles(root, files); err != nil {
		t.Fatal(err)
	}
	runtime, err = loadSubscriptionRuntime(root)
	if err != nil || runtime.Deliveries["device"].Digest != "ack" {
		t.Fatal("apply erased acknowledgement", err)
	}
	config.Deliveries[0].URL += "/changed"
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), config); err != nil {
		t.Fatal(err)
	}
	results[0].Persisted = false
	if _, err := checkpointDeliveries(t.Context(), root, results); err != nil || results[0].Persisted {
		t.Fatal("ack attached to replacement", err)
	}
}

func TestDeliveryDoesNotBlockConfigurationQueue(t *testing.T) {
	store, err := controlstore.Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Initialize(t.Context(), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	service := PlatformService{Store: store}
	if _, err := service.queueAutomaticSubscription(t.Context(), "deliver"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queue(t.Context(), "apply", "test", []byte("{}"), 1, []byte("{}")); err != nil {
		t.Fatal("delivery blocks config", err)
	}
	if task, err := store.ClaimLane(t.Context(), "background"); err != nil || task.Kind != "subscription" {
		t.Fatal(task, err)
	}
}

func TestDeliveryAcknowledgementFailureIsUncertain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rejected" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Length", "128")
		w.WriteHeader(http.StatusOK)
		// The remote accepted the upload, but its acknowledgement was truncated.
	}))
	defer server.Close()
	destination := sub.Delivery{Kind: "http-put", URL: server.URL}
	err := sendSubscriptionArtifact(t.Context(), server.Client(), destination, sub.Artifact{Data: []byte("output")})
	if !errors.Is(err, errDeliveryUncertain) {
		t.Fatal("ambiguous acknowledgement reported as definite failure", err)
	}
	destination.URL += "/rejected"
	err = sendSubscriptionArtifact(t.Context(), server.Client(), destination, sub.Artifact{Data: []byte("output")})
	if err == nil || errors.Is(err, errDeliveryUncertain) {
		t.Fatal("explicit rejection must remain distinct", err)
	}
}
