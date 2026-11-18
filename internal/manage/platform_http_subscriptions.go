package manage

import (
	"net/http"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func (s *PlatformService) serveGrants(w http.ResponseWriter, r *http.Request, access panelAccess) bool {
	ctx, path := r.Context(), r.URL.Path
	admin, require, audit := access.admin, access.require, access.audit
	var err error
	if path == "/api/clients" && r.Method == "GET" {
		jsonResponse(w, sub.Capabilities())
		return true
	}
	if path == "/api/grants" {
		if !require(admin) {
			return true
		}
		if r.Method == "GET" {
			query, e := pageQuery(r)
			if e != nil {
				platformError(w, e)
				return true
			}
			v, e := s.Accounts.GrantPage(ctx, query.Search, query.Offset, query.Limit)
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, v)
			}
			return true
		}
		if r.Method == "POST" {
			var g accounting.Grant
			if err = decodeRequest(w, r, &g, 8192); err == nil {
				if !audit("download-grant", g.ID) {
					return true
				}
				var token string
				var origin string
				origin, err = s.downloadOrigin(ctx, access.publicURL)
				if err == nil && len(g.Outputs) > 0 {
					err = s.validateOutputGrant(ctx, g.Outputs)
				}
				if err == nil {
					token, err = s.Accounts.CreateGrant(ctx, g)
				}
				if err == nil {
					urls := []string{}
					for _, id := range g.Outputs {
						urls = append(urls, origin+"/s/"+token+"/"+id)
					}
					userURL := ""
					if g.AccountID != "" {
						userURL = origin + "/user#" + token
					}
					jsonResponse(w, Object{"token": token, "user_url": userURL, "urls": urls})
					return true
				}
			}
			platformError(w, err)
			return true
		}
	}
	if path == "/api/grants/revoke" && r.Method == "POST" {
		if !require(admin) {
			return true
		}
		var v struct {
			ID string `json:"id"`
		}
		if err = decodeRequest(w, r, &v, 1024); err == nil {
			if !audit("grant-revoke", v.ID) {
				return true
			}
			err = s.Accounts.RevokeGrant(ctx, v.ID)
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
