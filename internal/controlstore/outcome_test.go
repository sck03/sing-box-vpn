package controlstore

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestFailedApplyDoesNotKeepPassingOutcome(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Initialize(t.Context(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	id, err := store.Queue(t.Context(), "apply", domain.TaskSubmission{Actor: "admin", Source: domain.TaskUser}, []byte(`{}`), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Result(t.Context(), id, []byte(`{"outcome":"passed","outputs":2}`)); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(t.Context(), id, errors.New("service failed; rolled back")); err != nil {
		t.Fatal(err)
	}
	task, err := store.Task(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(task.Result, &result); err != nil {
		t.Fatal(err)
	}
	if task.State != "failed" || task.Outcome != "failed" || result["outcome"] != "failed" || result["outputs"] != float64(2) {
		t.Fatal(task, result)
	}
}
