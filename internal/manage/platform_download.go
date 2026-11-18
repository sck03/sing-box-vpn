package manage

import (
	"errors"
	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/clientprofile"
	"net/http"
	"slices"
	"strings"
)

func (service AccountService) ServeDownload(w http.ResponseWriter, r *http.Request) {
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
	if len(token) != 64 || (resource != "usage" && !clientprofile.SupportsAccount(resource)) {
		http.NotFound(w, r)
		return
	}
	store, closeStore, err := service.openStore()
	if err != nil {
		http.Error(w, "account service unavailable", 503)
		return
	}
	defer closeStore()
	grant, usage, err := store.Grant(r.Context(), token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	denial := usage.DownloadDenial(grant)
	if resource == "usage" {
		jsonResponse(w, Object{"name": usage.Name, "state": usage.State, "quota_bytes": usage.QuotaBytes, "charged_bytes": usage.Charged, "upload_bytes": usage.Upload, "download_bytes": usage.Download, "effective_expires_at": usage.EffectiveExpiresAt, "started_at": usage.StartedAt, "next_reset": usage.NextReset, "download_expires_at": grant.ExpiresAt, "downloads": grant.Downloads, "max_downloads": grant.MaxDownloads, "clients": grant.Clients, "download_denial": denial})
		return
	}
	if !slices.Contains(grant.Clients, resource) {
		http.NotFound(w, r)
		return
	}
	if denial != "" {
		http.Error(w, denial, http.StatusForbidden)
		return
	}
	service.Store = store
	artifact, err := service.Export(r.Context(), application.AccountCommand{ID: grant.AccountID, Scope: grant.Scope, Client: resource})
	if errors.Is(err, accounting.ErrDenied) {
		http.Error(w, "account unavailable", http.StatusForbidden)
		return
	}
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
	w.Header().Set("Content-Type", artifact.MIME+"; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="account-`+resource+`.txt"`)
	if r.Method == "GET" {
		_, _ = w.Write(artifact.Data)
	}
}

func unifiedDownloadHandler(service AccountService) http.Handler {
	outputs := subscriptionHandler(service.Root, service.Store)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/u/") {
			service.ServeDownload(w, r)
			return
		}
		if r.URL.Path == "/user" || r.URL.Path == "/panel/user.js" || r.URL.Path == "/panel/style.css" || r.URL.Path == "/panel/theme.js" {
			name := strings.TrimPrefix(r.URL.Path, "/")
			if name == "user" {
				name = "panel/user.html"
			}
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'")
			servePanelAsset(w, r, name)
			return
		}
		outputs.ServeHTTP(w, r)
	})
}
