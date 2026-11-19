package manage

import (
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"net/http"
	"time"
)

func (s *PlatformService) serveTaskMaintenance(w http.ResponseWriter, r *http.Request, access panelAccess) bool {
	path := r.URL.Path
	if path != "/api/tasks/history" && path != "/api/tasks/history/clear" && path != "/api/tasks/history/prune" && path != "/api/tasks/delete" && path != "/api/tasks/acknowledge" {
		return false
	}
	if !access.require(access.admin) {
		return true
	}
	if path == "/api/tasks/history" && r.Method == "GET" {
		p, err := s.Store.HistoryPolicy(r.Context())
		u, e := s.Store.HistoryUsage(r.Context())
		if err != nil {
			platformError(w, err)
		} else if e != nil {
			platformError(w, e)
		} else {
			jsonResponse(w, Object{"policy": p, "usage": u})
		}
		return true
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	if !access.audit("task-history", path) {
		return true
	}
	var err error
	var deleted int64
	switch path {
	case "/api/tasks/history":
		var p controlstore.HistoryPolicy
		if err = decodeRequest(w, r, &p, 1024); err == nil {
			err = s.Store.SetHistoryPolicy(r.Context(), p)
		}
	case "/api/tasks/history/prune":
		err = s.Store.PruneHistory(r.Context(), time.Now())
	case "/api/tasks/history/clear":
		var v struct {
			AcknowledgedOnly bool `json:"acknowledged_only"`
		}
		if err = decodeRequest(w, r, &v, 1024); err == nil {
			deleted, err = s.Store.ClearTasks(r.Context(), v.AcknowledgedOnly)
		}
	default:
		var v struct {
			ID int64 `json:"id"`
		}
		if err = decodeRequest(w, r, &v, 1024); err == nil {
			if v.ID < 1 {
				http.Error(w, "invalid task ID", 400)
				return true
			}
			if path == "/api/tasks/delete" {
				err = s.Store.DeleteTask(r.Context(), v.ID)
			} else {
				err = s.Store.Acknowledge(r.Context(), v.ID)
			}
		}
	}
	if err != nil {
		platformError(w, err)
	} else {
		s.invalidateOverview()
		jsonResponse(w, Object{"ok": true, "deleted": deleted})
	}
	return true
}
