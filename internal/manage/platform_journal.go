package manage

import (
	"github.com/sck03/sing-box-vpn/internal/hostservice"
	"net/http"
	"strconv"
)

func (s *PlatformService) serveJournal(w http.ResponseWriter, r *http.Request, access panelAccess) bool {
	path := r.URL.Path
	if path != "/api/logs" && path != "/api/logs/usage" && path != "/api/logs/clear" && path != "/api/logs/download" {
		return false
	}
	if !access.require(access.operator) {
		return true
	}
	if s.NoSystem {
		http.Error(w, "系统日志需要 Linux systemd", 400)
		return true
	}
	if path == "/api/logs/clear" && r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	if path == "/api/logs/clear" && r.Method == "POST" {
		if !access.require(access.admin) || !access.audit("journal-clear", "sb-vpn") {
			return true
		}
		if err := hostservice.ClearJournal(r.Context(), hostCommand); err != nil {
			platformError(w, err)
		} else {
			jsonResponse(w, Object{"ok": true})
		}
		return true
	}
	if r.Method != "GET" {
		http.NotFound(w, r)
		return true
	}
	if !access.audit("journal-read", r.URL.Query().Get("service")) {
		return true
	}
	if path == "/api/logs/usage" {
		v, err := hostservice.JournalUsage(r.Context(), hostCommand)
		if err != nil {
			platformError(w, err)
		} else {
			jsonResponse(w, Object{"usage": v, "namespace": "sb-vpn", "max_bytes": 64 << 20, "retention_days": 7})
		}
		return true
	}
	q := hostservice.JournalQuery{Service: r.URL.Query().Get("service"), Since: r.URL.Query().Get("since")}
	if raw := r.URL.Query().Get("lines"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			platformError(w, err)
			return true
		}
		q.Lines = n
	}
	v, err := hostservice.Journal(r.Context(), hostCommand, q)
	if err != nil {
		platformError(w, err)
	} else if path == "/api/logs/download" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="sb-vpn.log"`)
		_, _ = w.Write([]byte(v))
	} else {
		jsonResponse(w, Object{"log": v})
	}
	return true
}
