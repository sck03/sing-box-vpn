package manage

import (
	"encoding/base64"
	"net/http"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
	qrcode "github.com/skip2/go-qrcode"
)

func (s *PlatformService) serveAccounts(w http.ResponseWriter, r *http.Request, access panelAccess) bool {
	ctx, path := r.Context(), r.URL.Path
	admin, require, audit := access.admin, access.require, access.audit
	var err error
	if path == "/api/inbound-options" && r.Method == "GET" {
		if !require(admin) {
			return true
		}
		state, e := s.Store.State(ctx)
		if e != nil {
			platformError(w, e)
			return true
		}
		config, e := decodePlatform(state.Applied)
		if e != nil {
			platformError(w, e)
			return true
		}
		scope := r.URL.Query().Get("scope")
		if scope == vpngateScope && config.VPN.ActiveCountry == "" {
			jsonResponse(w, []application.Option{})
			return true
		}
		entries, e := configuredEntries(config, scope)
		if e != nil {
			platformError(w, e)
			return true
		}
		options := []application.Option{}
		for _, entry := range entries {
			if !entry.Disabled {
				options = append(options, application.Option{ID: entry.ID, Name: entry.ID + " · " + entry.Protocol})
			}
		}
		jsonResponse(w, options)
		return true
	}
	if path == "/api/qr" && r.Method == "POST" {
		if !require(admin) {
			return true
		}
		var v struct {
			Content string `json:"content"`
		}
		if err = decodeRequest(w, r, &v, 4096); err != nil {
			platformError(w, err)
			return true
		}
		if len(v.Content) > 2048 {
			http.Error(w, "QR content exceeds 2048 bytes", 400)
			return true
		}
		png, e := qrcode.Encode(v.Content, qrcode.Medium, 320)
		if e != nil {
			platformError(w, e)
			return true
		}
		jsonResponse(w, Object{"image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)})
		return true
	}
	if path == "/api/accounts/clients" && r.Method == "GET" {
		if !require(admin) {
			return true
		}
		options, e := s.accountService().ClientOptions(ctx, r.URL.Query().Get("id"), r.URL.Query().Get("scope"))
		if e != nil {
			platformError(w, e)
		} else {
			jsonResponse(w, options)
		}
		return true
	}
	if path == "/api/accounts/export" && r.Method == "POST" {
		if !require(admin) || !audit("account-export", "") {
			return true
		}
		var v application.AccountCommand
		if err = decodeRequest(w, r, &v, 8192); err != nil {
			platformError(w, err)
			return true
		}
		v.Operation = "export"
		result, e := s.accountService().ExportDetailed(ctx, v)
		if e != nil {
			platformError(w, e)
			return true
		}
		jsonResponse(w, Object{"content": string(result.Artifact.Data), "mime": result.Artifact.MIME, "extension": result.Artifact.Extension, "client": result.Client, "validation": result.Validation})
		return true
	}
	if path == "/api/account-options" && r.Method == "GET" {
		if !require(admin) {
			return true
		}
		v, e := s.Accounts.AccountOptions(ctx)
		if e != nil {
			platformError(w, e)
		} else {
			jsonResponse(w, v)
		}
		return true
	}
	if path == "/api/accounts/archive" && r.Method == "POST" {
		if !require(admin) {
			return true
		}
		var v struct {
			ID       string `json:"id"`
			Revision int64  `json:"revision"`
		}
		if err = decodeRequest(w, r, &v, 1024); err == nil {
			if !audit("account-archive", v.ID) {
				return true
			}
			err = s.Accounts.ArchiveAccount(ctx, v.ID, v.Revision)
		}
		if err != nil {
			platformError(w, err)
		} else {
			jsonResponse(w, Object{"ok": true})
		}
		return true
	}
	if path == "/api/accounts" {
		if r.Method == "GET" {
			q, e := pageQuery(r)
			if e != nil {
				platformError(w, e)
				return true
			}
			v, e := s.shareAccountPage(ctx, q, r.URL.Query().Get("archived") == "true")
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, v)
			}
			return true
		} else if r.Method == "POST" {
			if !require(admin) {
				return true
			}
			var v domainAccountRequest
			if err = decodeRequest(w, r, &v, 64<<10); err == nil {
				if !audit("account-save", v.Account.ID) {
					return true
				}
				err = s.Accounts.SaveAccount(ctx, v.Account)
				if err == nil {
					jsonResponse(w, Object{"ok": true})
					return true
				}
			}
		}
		if err != nil {
			platformError(w, err)
			return true
		}
	}
	if path == "/api/subscribers" {
		if r.Method == "GET" {
			v, e := s.Accounts.Subscribers(ctx)
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, v)
			}
			return true
		}
		if r.Method == "POST" {
			if !require(admin) {
				return true
			}
			var v accounting.Subscriber
			if err = decodeRequest(w, r, &v, 4096); err == nil {
				if !audit("subscriber-save", v.ID) {
					return true
				}
				err = s.Accounts.SaveSubscriber(ctx, v)
			}
			if err != nil {
				platformError(w, err)
			} else {
				jsonResponse(w, Object{"ok": true})
			}
			return true
		}
	}
	if path == "/api/credentials" && r.Method == "GET" {
		if !require(admin) {
			return true
		}
		var credentials []CredentialSummary
		credentials, err = s.accountService().Credentials(ctx, r.URL.Query().Get("id"), r.URL.Query().Get("scope"))
		if err != nil {
			platformError(w, err)
		} else {
			jsonResponse(w, credentials)
		}
		return true
	}
	if path == "/api/history" && r.Method == "GET" {
		v, e := s.Accounts.History(ctx, r.URL.Query().Get("id"))
		if e != nil {
			platformError(w, e)
		} else {
			jsonResponse(w, v)
		}
		return true
	}
	return false
}

type domainAccountRequest struct {
	Account domain.Account `json:"account"`
}
