package manage

import (
	"errors"
	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/clientprofile"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
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
	store, err := service.requireStore()
	if err != nil {
		http.Error(w, "account service unavailable", 503)
		return
	}
	grant, usage, err := store.Grant(r.Context(), token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	denial := usage.DownloadDenial(grant)
	if resource == "usage" {
		denial, err = service.grantDenial(r.Context(), store, usage, grant)
		if err != nil {
			http.Error(w, "account service unavailable", 503)
			return
		}
		clients := []AccountClientOption{}
		if denial == "" {
			service.Store = store
			options, e := service.ClientOptions(r.Context(), grant.AccountID, grant.Scope, append([]string{}, grant.Clients...))
			if e != nil {
				http.Error(w, "account service unavailable", 503)
				return
			}
			clients = options
		}
		jsonResponse(w, Object{"name": usage.Name, "state": usage.State, "quota_bytes": usage.QuotaBytes, "charged_bytes": usage.Charged, "upload_bytes": usage.Upload, "download_bytes": usage.Download, "effective_expires_at": usage.EffectiveExpiresAt, "started_at": usage.StartedAt, "start_mode": usage.StartMode, "next_reset": usage.NextReset, "download_expires_at": grant.ExpiresAt, "downloads": grant.Downloads, "max_downloads": grant.MaxDownloads, "clients": clients, "download_denial": denial})
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
	result, err := service.ExportDetailed(r.Context(), application.AccountCommand{ID: grant.AccountID, Scope: grant.Scope, Client: resource})
	if errors.Is(err, errNoBoundInbound) {
		http.Error(w, "no_bound_inbound", http.StatusForbidden)
		return
	}
	if errors.Is(err, accounting.ErrDenied) {
		http.Error(w, "account unavailable", http.StatusForbidden)
		return
	}
	if err != nil {
		if result.Client.Reason != "" {
			http.Error(w, result.Client.Reason, http.StatusUnprocessableEntity)
		} else {
			http.Error(w, "account output unavailable", 503)
		}
		return
	}
	artifact := result.Artifact
	// The atomic reservation happens after rendering, immediately before sending.
	if r.Method == "GET" {
		if err = store.CountDownload(r.Context(), token, resource); err != nil {
			http.Error(w, "download authorization exhausted or revoked", 403)
			return
		}
	}
	w.Header().Set("Content-Type", artifact.MIME+"; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="account-`+resource+`.`+clientprofile.Extension(resource)+`"`)
	w.Header().Set("profile-update-interval", "1")
	w.Header().Set("subscription-userinfo", accountSubscriptionUsage(result.Usage))
	w.Header().Set("Content-Length", strconv.Itoa(len(artifact.Data)))
	if r.Method == "GET" {
		_, _ = w.Write(artifact.Data)
	}
}

func accountSubscriptionUsage(usage accounting.Usage) string {
	upload, download := uint64(max(0, usage.Upload)), uint64(max(0, usage.Download))
	value := sub.Usage{Upload: &upload, Download: &download}
	if usage.QuotaBytes != nil {
		total := uint64(max(0, *usage.QuotaBytes))
		value.Total = &total
	}
	if usage.EffectiveExpiresAt != nil && usage.EffectiveExpiresAt.After(time.Unix(0, 0)) {
		expiry := usage.EffectiveExpiresAt.Unix()
		value.Expire = &expiry
	}
	return formatSubscriptionUsage(value)
}

func unifiedDownloadHandler(service AccountService) http.Handler {
	if service.exports == nil {
		service.exports = &accountExportCache{}
	}
	if service.view == nil {
		service.view = &subscriptionView{}
	}
	outputs := subscriptionHandlerView(service.Root, service.view, service.Store)
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
