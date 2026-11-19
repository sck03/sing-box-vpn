package manage

import (
	"net/http"
	"strconv"
)

func (s *PlatformService) serveTasks(w http.ResponseWriter, r *http.Request, access panelAccess) bool {
	if s.serveTaskMaintenance(w, r, access) || s.serveJournal(w, r, access) {
		return true
	}
	ctx, path := r.Context(), r.URL.Path
	admin, operator, require, audit := access.admin, access.operator, access.require, access.audit
	var err error
	if path == "/api/tasks" && r.Method == "GET" {
		if !require(operator) {
			return true
		}
		before, limit := int64(0), int64(50)
		for key, target := range map[string]*int64{"before": &before, "limit": &limit} {
			if value := r.URL.Query().Get(key); value != "" {
				*target, err = strconv.ParseInt(value, 10, 64)
				if err != nil || *target < 0 || (key == "limit" && (*target < 1 || *target > 100)) {
					apiFailure(w, 400, "invalid_field", "invalid task page", false, key)
					return true
				}
			}
		}
		page, err := s.Store.TaskPage(ctx, before, int(limit))
		if err != nil {
			platformError(w, err)
		} else {
			jsonResponse(w, page)
		}
		return true
	}
	if path == "/api/tasks/detail" && r.Method == "GET" {
		if !require(operator) {
			return true
		}
		id, e := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		if e != nil || id < 1 {
			apiFailure(w, 400, "invalid_field", "invalid task ID", false, "id")
			return true
		}
		v, e := s.Store.Task(ctx, id)
		if e != nil {
			platformError(w, e)
			return true
		}
		role := "operator"
		if admin {
			role = "admin"
		}
		if !require(v.Details.Allows(role, access.actor)) {
			return true
		}
		jsonResponse(w, v)
		return true
	}
	if path == "/api/tasks" && r.Method == "POST" {
		var a PlatformAction
		if err = decodeRequest(w, r, &a, 1<<20); err != nil {
			platformError(w, err)
			return true
		}
		if !require(admin || (operator && (a.Operation == "service" || a.Operation == "gateway"))) {
			return true
		}
		if !audit(a.Operation, a.ID) {
			return true
		}
		id, e := s.Queue(ctx, a, access.actor)
		if e != nil {
			platformError(w, e)
		} else {
			jsonResponse(w, QueuedTask{TaskID: id})
		}
		return true
	}
	if path == "/api/tasks/cancel" && r.Method == "POST" {
		if !require(admin) {
			return true
		}
		var v struct {
			ID int64 `json:"id"`
		}
		if err = decodeRequest(w, r, &v, 1024); err == nil {
			if !audit("task-cancel", strconv.FormatInt(v.ID, 10)) {
				return true
			}
			err = s.Store.Cancel(ctx, v.ID)
		}
		if err != nil {
			platformError(w, err)
		} else {
			jsonResponse(w, Object{"ok": true})
		}
		return true
	}
	return false
}
