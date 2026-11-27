package manage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
)

func TestHTTPTaskSubmissionRetryAndSchedulerUsername(t *testing.T) {
	s, err := OpenPlatform(t.TempDir(), "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Store.Initialize(t.Context(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.Auth.Bootstrap(t.Context(), "scheduler", "review-password-123", false); err != nil {
		t.Fatal(err)
	}
	session, err := s.Auth.Login(t.Context(), "scheduler", "review-password-123", "", "local")
	if err != nil {
		t.Fatal(err)
	}
	handler := s.Handler(PanelOptions{PublicURL: "http://example.com"})
	send := func(key, body string) (int, int64) {
		r := panelRequest("POST", "/api/tasks", []byte(body), session)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var result QueuedTask
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		return w.Code, result.TaskID
	}
	body := `{"operation":"service","service":"sb-core","value":"restart"}`
	code, id := send("request-0000000001", body)
	if code != 200 || id < 1 {
		t.Fatal(code, id)
	}
	code, retry := send("request-0000000001", `{"value":"restart", "service":"sb-core", "operation":"service"}`)
	if code != 200 || retry != id {
		t.Fatal(code, retry)
	}
	code, next := send("request-0000000002", body)
	if code != 200 || next <= id {
		t.Fatal(code, next)
	}
	code, _ = send("request-0000000001", `{"operation":"service","service":"sb-core","value":"stop"}`)
	if code != 409 {
		t.Fatal(code)
	}
	code, _ = send("bad", body)
	if code != 400 {
		t.Fatal(code)
	}
	if err = s.Store.Finish(t.Context(), id, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.DeleteTask(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	code, _ = send("request-0000000001", body)
	if code != 410 {
		t.Fatal(code)
	}
}

func TestSchedulerFailureAuditsAfterOperationDeadline(t *testing.T) {
	auth, err := adminauth.Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer auth.Close()
	service := &PlatformService{Auth: auth}
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	service.schedulerResult(ctx, "task-retention", context.DeadlineExceeded)
	issues := service.health.snapshot()
	if len(issues) != 1 || issues[0].Operation != "task-retention" || issues[0].Code != "timeout" {
		t.Fatal(issues)
	}
	service.schedulerResult(t.Context(), "task-retention", nil)
	if len(service.health.snapshot()) != 0 {
		t.Fatal("successful cleanup did not clear its issue")
	}
	if err = auth.Close(); err != nil {
		t.Fatal(err)
	}
	service.schedulerResult(ctx, "task-retention", errors.New("storage unavailable"))
	if len(service.health.snapshot()) != 2 {
		t.Fatal("audit failure was hidden", service.health.snapshot())
	}
}

func TestApplySubmissionReplayPrecedesCandidatePreparation(t *testing.T) {
	service, err := OpenPlatform(t.TempDir(), "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if err = service.Store.Initialize(t.Context(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "http://example.com/api/config", nil)
	request.Header.Set("Idempotency-Key", "configuration-retry-0001")
	submission, err := taskSubmission(request, "admin", ConfigurationSave{})
	if err != nil {
		t.Fatal(err)
	}
	id, err := service.Store.Queue(t.Context(), "apply", submission, []byte(`{}`), 1, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	retry, err := service.QueueApply(t.Context(), PlatformConfig{}, 1, submission)
	if err != nil || retry != id {
		t.Fatal(retry, err)
	}
	submission.Fingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err = service.QueueApply(t.Context(), PlatformConfig{}, 1, submission); !errors.Is(err, controlstore.ErrRequestConflict) {
		t.Fatal(err)
	}
}
