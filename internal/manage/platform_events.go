package manage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Snapshot construction is shared across viewers and only occurs on demand.
// No per-viewer queue grows while a browser is slow or hidden.
func (s *PlatformService) overviewSnapshot(ctx context.Context) ([]byte, error) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if time.Since(s.statusAt) < 4*time.Second && s.status != nil {
		return s.status, nil
	}
	state, err := s.Store.State(ctx)
	if err != nil {
		return nil, err
	}
	services, err := s.ServiceStatus(ctx)
	if err != nil {
		return nil, err
	}
	runtimes, err := s.Store.Runtimes(ctx)
	if err != nil {
		return nil, err
	}
	tasks, err := s.Store.TaskSummaries(ctx)
	if err != nil {
		return nil, err
	}
	// Overview is shared across all roles and refreshed frequently. Detailed
	// results are fetched explicitly by authorized task viewers.
	for i := range tasks {
		tasks[i].Result = nil
		if tasks[i].Error != "" {
			tasks[i].Error = "任务失败，请查看任务详情"
		}
	}
	data, err := json.Marshal(Object{"revision": state.Revision, "applied_revision": state.AppliedRevision, "services": services, "runtimes": runtimes, "tasks": tasks})
	if err == nil {
		s.status, s.statusAt = data, time.Now()
	}
	return data, err
}
func (s *PlatformService) streamOverview(w http.ResponseWriter, r *http.Request, token string) {
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if _, err := s.Auth.Authenticate(r.Context(), token, "", false); err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		data, err := s.overviewSnapshot(ctx)
		cancel()
		if err != nil {
			return
		}
		if err = controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return
		}
		if _, err = fmt.Fprintf(w, "event: overview\ndata: %s\n\n", data); err != nil {
			return
		}
		if err = controller.Flush(); err != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
