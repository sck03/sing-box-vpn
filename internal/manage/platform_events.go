package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Snapshot construction is shared across viewers and only occurs on demand.
// No per-viewer queue grows while a browser is slow or hidden.
func (s *PlatformService) invalidateOverview() {
	s.statusMu.Lock()
	s.statusAt = time.Time{}
	s.statusVersion++
	s.statusMu.Unlock()
}

func (s *PlatformService) overviewSnapshot(ctx context.Context) ([]byte, error) {
	for {
		s.statusMu.Lock()
		if time.Since(s.statusAt) < 4*time.Second && s.status != nil {
			data := s.status
			s.statusMu.Unlock()
			return data, nil
		}
		if pending := s.statusLoading; pending != nil {
			s.statusMu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		pending, version := make(chan struct{}), s.statusVersion
		s.statusLoading = pending
		s.statusMu.Unlock()
		child, cancel := context.WithTimeout(ctx, 10*time.Second)
		data, err := s.collectOverview(child)
		cancel()
		s.statusMu.Lock()
		if err == nil && version == s.statusVersion {
			s.status, s.statusAt = data, time.Now()
		}
		s.statusLoading = nil
		close(pending)
		s.statusMu.Unlock()
		return data, err
	}
}

func (s *PlatformService) collectOverview(ctx context.Context) ([]byte, error) {
	state, err := s.Store.Revisions(ctx)
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
	// The overview exposes liveness, not a changing counter. Keep exact heartbeats
	// in controlstore for readiness checks; idle streams can reuse their snapshot.
	for i := range runtimes {
		runtimes[i].Heartbeat = 0
	}
	tasks, err := s.Store.RecentTaskSummaries(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.Store.TaskCounts(ctx)
	if err != nil {
		return nil, err
	}
	publications, err := s.Accounts.CredentialPublications(ctx)
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
	return json.Marshal(OverviewSnapshot{Background: s.health.snapshot(), Revision: state.Revision, AppliedRevision: state.AppliedRevision, Services: services, Runtimes: runtimes, Tasks: tasks, TaskCounts: counts, Credentials: publications})
}
func (s *PlatformService) streamOverview(w http.ResponseWriter, r *http.Request, token string) {
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var previous []byte
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
		if bytes.Equal(previous, data) {
			_, err = fmt.Fprint(w, "event: tick\ndata: {}\n\n")
		} else {
			_, err = fmt.Fprintf(w, "event: overview\ndata: %s\n\n", data)
		}
		if err != nil {
			return
		}
		if err = controller.Flush(); err != nil {
			return
		}
		previous = data
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
