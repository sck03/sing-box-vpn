package manage

import (
	"bytes"
	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/database"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
)

func serveAccountDownload(root string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "method not allowed", 405)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/u/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	token, resource := parts[0], parts[1]
	store, err := accounting.Open(filepath.Join(root, database.Filename))
	if err != nil {
		http.Error(w, "account service unavailable", 503)
		return
	}
	defer store.Close()
	grant, usage, err := store.Grant(r.Context(), token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if resource == "usage" {
		jsonResponse(w, Object{"name": usage.Name, "state": usage.State, "quota_bytes": usage.QuotaBytes, "charged_bytes": usage.Charged, "upload_bytes": usage.Upload, "download_bytes": usage.Download, "expires_at": usage.ExpiresAt, "started_at": usage.StartedAt, "next_reset": usage.NextReset, "download_expires_at": grant.ExpiresAt, "downloads": grant.Downloads, "max_downloads": grant.MaxDownloads, "clients": grant.Clients})
		return
	}
	if !slices.Contains(grant.Clients, resource) {
		http.NotFound(w, r)
		return
	}
	var out bytes.Buffer
	err = (AccountService{Root: root}).Execute(r.Context(), application.AccountCommand{Operation: "export", ID: grant.AccountID, Scope: grant.Scope, Client: resource}, &out)
	if err != nil {
		http.Error(w, "account output unavailable", 503)
		return
	}
	// The atomic reservation happens after rendering, immediately before sending.
	if r.Method == "GET" {
		if err = store.CountDownload(r.Context(), token, resource); err != nil {
			http.Error(w, "download authorization exhausted or revoked", 403)
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if resource == "sing-box" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.Header().Set("Content-Disposition", `attachment; filename="account-`+resource+`.txt"`)
	if r.Method == "GET" {
		_, _ = w.Write(out.Bytes())
	}
}

func unifiedDownloadHandler(root string) http.Handler {
	legacy := subscriptionHandler(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/u/") {
			serveAccountDownload(root, w, r)
			return
		}
		if r.URL.Path == "/user" || r.URL.Path == "/panel/user.js" || r.URL.Path == "/panel/style.css" {
			name := strings.TrimPrefix(r.URL.Path, "/")
			if name == "user" {
				name = "panel/user.html"
			}
			data, err := panelAssets.ReadFile(name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'")
			mime := "text/html; charset=utf-8"
			if strings.HasSuffix(name, ".js") {
				mime = "text/javascript"
			}
			if strings.HasSuffix(name, ".css") {
				mime = "text/css"
			}
			w.Header().Set("Content-Type", mime)
			_, _ = w.Write(data)
			return
		}
		legacy.ServeHTTP(w, r)
	})
}
