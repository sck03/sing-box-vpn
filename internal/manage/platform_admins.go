package manage

import (
	"net/http"
)

func (s *PlatformService) serveAdministrators(w http.ResponseWriter, r *http.Request, actor string) {
	ctx := r.Context()
	path := r.URL.Path
	if r.Method == "GET" {
		switch path {
		case "/api/admins":
			v, e := s.Auth.List(ctx)
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, v)
			}
			return
		case "/api/admin/audit":
			v, e := s.Auth.AuditLog(ctx)
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, v)
			}
			return
		}
	}
	if r.Method != "POST" {
		apiFailure(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", false)
		return
	}
	var v AdministratorInput
	if err := decodeRequest(w, r, &v, 8192); err != nil {
		platformError(w, err)
		return
	}
	var err error
	switch path {
	case "/api/admins":
		err = s.Auth.Save(ctx, actor, v.Admin, v.Password)
	case "/api/admin/revoke":
		if err = s.Auth.Audit(ctx, actor, "sessions-revoke", v.ID, "requested"); err == nil {
			err = s.Auth.Revoke(ctx, v.ID)
		}
	case "/api/mfa/enroll":
		var secret string
		secret, err = s.Auth.Enroll(ctx, actor, v.Password)
		if err == nil {
			jsonResponse(w, MFAEnrollment{secret, "sing-box-vpn", actor, 600})
			return
		}
	case "/api/mfa/confirm":
		err = s.Auth.ConfirmMFA(ctx, actor, v.Code)
	default:
		apiFailure(w, http.StatusNotFound, "not_found", "not found", false)
		return
	}
	if err != nil {
		platformError(w, err)
	} else {
		jsonResponse(w, ActionResult{OK: true})
	}
}
