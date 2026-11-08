package manage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "jhsub.txt"), []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			var settings remoteSettings
			if err := json.Unmarshal([]byte(`{"telegram":{"token":"test-token","chat_id":"test-chat"}}`), &settings); err != nil {
				t.Fatal(err)
			}
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
			err := pushTelegram(context.Background(), client, "https://example.test", root, normalScope, settings)
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

func TestGitLabUsesOneExportSnapshot(t *testing.T) {
	root := t.TempDir()
	names := []string{"sbox.json", "clmi.yaml", "jhsub.txt"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(root, name), []byte("before"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var settings remoteSettings
	if err := json.Unmarshal([]byte(`{"gitlab":{"project":"group/project","branch":"main","token":"test-token"}}`), &settings); err != nil {
		t.Fatal(err)
	}
	var commits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repository/commits"):
			commits++
			var body struct {
				Actions []struct {
					Content string `json:"content"`
				} `json:"actions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body.Actions) != len(names) {
				t.Error("incomplete subscription commit")
			}
			for _, action := range body.Actions {
				if action.Content != "before" {
					t.Errorf("push mixed a newer export into its snapshot: %q", action.Content)
				}
			}
			io.WriteString(w, `{}`)
		case strings.Contains(r.URL.Path, "/repository/files/"):
			if r.Method != http.MethodHead {
				t.Error("file existence check downloaded content instead of using HEAD")
			}
			// This must succeed while the remote request is active: no disk lock
			// may span network I/O, and later files must still use the snapshot.
			if err := withLock(root, func() error {
				bundle := fileBundle{}
				for _, name := range names {
					bundle[name] = []byte("after")
				}
				return publish(root, bundle)
			}); err != nil {
				t.Error("remote request held configuration lock:", err)
			}
			w.WriteHeader(http.StatusNotFound)
		default:
			fmt.Fprint(w, `{"visibility":"private"}`)
		}
	}))
	defer server.Close()
	if err := pushGitLab(context.Background(), server.Client(), server.URL, root, normalScope, settings); err != nil {
		t.Fatal(err)
	}
	if commits != 1 {
		t.Fatal("expected one atomic remote commit", commits)
	}
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

func TestRemoteSnapshotRecoversInterruptedPublication(t *testing.T) {
	root := t.TempDir()
	before := fileBundle{"jhsub.txt": []byte("normal"), "vpngate/jhsub.txt": []byte("vpn")}
	for name := range before {
		if err := AtomicBytes(filepath.Join(root, name), []byte("partial"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writePendingJournal(t, root, before, snapshotStage)
	for _, scope := range []string{vpngateScope, normalScope} {
		files, err := readRemoteExports(context.Background(), root, scope, "jhsub.txt")
		if err != nil || len(files) != 1 {
			t.Fatal("snapshot failed", err)
		}
		if files[0].content != string(before[files[0].path]) {
			t.Fatal("snapshot used partial or wrong-scope content", files)
		}
	}
	if _, err := os.Stat(filepath.Join(root, journalName)); !os.IsNotExist(err) {
		t.Fatal("pending publication was not recovered", err)
	}
}

func TestRemoteSnapshotLockWaitCanBeCancelled(t *testing.T) {
	root := t.TempDir()
	if err := withConfigLock(root, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		files, err := readRemoteExports(ctx, root, normalScope, "jhsub.txt")
		if !errors.Is(err, context.DeadlineExceeded) || files != nil {
			t.Errorf("lock wait ignored cancellation: files=%v, err=%v", files, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteExportBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export")
	if err := os.WriteFile(path, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	if content, err := readRemoteExport(path); err != nil || content != "content" {
		t.Fatal("valid export rejected", err)
	}
	for _, size := range []int64{maxRemoteExport, maxRemoteExport + 1} {
		if err := os.Truncate(path, size); err != nil {
			t.Fatal(err)
		}
		content, err := readRemoteExport(path)
		if size == maxRemoteExport {
			if err != nil || len(content) != maxRemoteExport {
				t.Fatal("exact file limit rejected", err)
			}
		} else if err == nil || content != "" {
			t.Fatal("oversized file accepted")
		}
	}
	if _, err := readRemoteExport(filepath.Dir(path)); err == nil {
		t.Fatal("directory accepted as an export")
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
