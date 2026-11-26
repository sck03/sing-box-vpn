package manage

import (
	"context"
	"encoding/json"
	"errors"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTelegramMessageBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		want          []string
		invalid       bool
	}{
		{"exact-limit", strings.Repeat("a", 3500), []string{strings.Repeat("a", 3500)}, false},
		{"unicode-limit", strings.Repeat("界", 1166) + "ab", []string{strings.Repeat("界", 1166) + "ab"}, false},
		{"combined-limit", strings.Repeat("a", 3498) + "\nb", []string{strings.Repeat("a", 3498) + "\nb"}, false},
		{"split", strings.Repeat("a", 3499) + "\nb", []string{strings.Repeat("a", 3499), "b"}, false},
		{"blank-lines", "\r\n first\r\n\r\nsecond\r\n", []string{"first\nsecond"}, false},
		{"empty", " \r\n\t", nil, true},
		{"oversized-tail", "first\n" + strings.Repeat("a", 3501), nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var messages []string
			client := &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
				var body struct {
					Text string `json:"text"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				messages = append(messages, body.Text)
				return byteResponse([]byte(`{"ok":true}`)), nil
			})}
			err := sendSubscriptionArtifact(t.Context(), client, sub.Delivery{Kind: "telegram", URL: "https://example.test", Token: "test-token", Username: "test-chat"}, sub.Artifact{Data: []byte(tc.content)})
			if (err != nil) != tc.invalid || !reflect.DeepEqual(messages, tc.want) {
				t.Fatalf("error=%v, message lengths=%v; want invalid=%v, lengths=%v", err, messageLengths(messages), tc.invalid, messageLengths(tc.want))
			}
		})
	}
}

func messageLengths(messages []string) []int {
	lengths := make([]int, len(messages))
	for i, message := range messages {
		lengths[i] = len(message)
	}
	return lengths
}

func BenchmarkRemoteAcknowledgement(b *testing.B) {
	data := []byte(`{"message":"` + strings.Repeat("a", 1<<20) + `"}`)
	client := &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		return byteResponse(data), nil
	})}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if err := remoteRequest(context.Background(), client, "https://example.test", http.MethodPost, nil, nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func TestDeliverySnapshotRecoversPublicationAndRespectsCancellation(t *testing.T) {
	root := t.TempDir()
	confirmSubscriptionFixture(t, root)
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), sub.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	snapshot := sub.Snapshot{Version: sub.SchemaVersion, Revision: 1, Artifacts: map[string]sub.Artifact{"normal": {Data: []byte("normal")}, "vpn": {Data: []byte("vpn")}}}
	before := fileBundle{}
	if err := before.json(subscriptionsSnapshot, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := AtomicBytes(filepath.Join(root, subscriptionsSnapshot), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	writePendingJournal(t, root, before, snapshotStage)
	_, _, captured, err := captureSubscriptionDelivery(t.Context(), root)
	if err != nil || !reflect.DeepEqual(captured.Artifacts, snapshot.Artifacts) {
		t.Fatal(captured, err)
	}
	if _, err := os.Stat(filepath.Join(root, journalName)); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := withConfigLock(root, func() error {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		_, _, _, err := captureSubscriptionDelivery(ctx, root)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("lock ignored cancellation: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The returned snapshot owns its bytes and holds no publication lock.
	if err := withLock(root, func() error { return AtomicJSON(filepath.Join(root, subscriptionsSnapshot), sub.Snapshot{}) }); err != nil {
		t.Fatal(err)
	}
	if string(captured.Artifacts["normal"].Data) != "normal" {
		t.Fatal("snapshot changed during delivery")
	}
	if _, _, _, err := captureSubscriptionDelivery(t.Context(), root); err == nil {
		t.Fatal("invalid publication accepted")
	}
}

type remoteTestBody struct {
	io.Reader
	closed bool
}

func (b *remoteTestBody) Close() error { b.closed = true; return nil }

type remoteErrorReader struct{}

func (remoteErrorReader) Read([]byte) (int, error) {
	return 0, errors.New("private response read error")
}

func TestRemoteAcknowledgementBoundsAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reader  io.Reader
		invalid bool
	}{
		{"exact-limit", strings.NewReader(strings.Repeat("x", 4<<20)), false},
		{"oversized", strings.NewReader(strings.Repeat("x", (4<<20)+1)), true},
		{"read-error", remoteErrorReader{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &remoteTestBody{Reader: tc.reader}
			client := &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
			})}
			err := remoteRequest(context.Background(), client, "https://example.test", http.MethodPost, nil, nil, nil)
			if (err != nil) != tc.invalid || !body.closed {
				t.Fatalf("error=%v closed=%v", err, body.closed)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("response read exposed private details")
			}
		})
	}
}
