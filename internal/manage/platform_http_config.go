package manage

import "net/http"

func (s *PlatformService) serveConfiguration(w http.ResponseWriter, r *http.Request, access panelAccess) bool {
	ctx, path := r.Context(), r.URL.Path
	admin, operator, require, audit := access.admin, access.operator, access.require, access.audit
	var err error
	if path == "/api/program" && r.Method == "GET" {
		if !require(admin) {
			return true
		}
		result, err := s.ProgramStatus(ctx, r.URL.Query().Get("check") == "true")
		if err != nil {
			platformError(w, err)
		} else {
			jsonResponse(w, result)
		}
		return true
	}
	if path == "/api/inbounds/impact" && r.Method == "GET" {
		if !require(admin) {
			return true
		}
		v, e := s.InboundImpact(ctx, r.URL.Query().Get("scope"), r.URL.Query().Get("id"))
		if e != nil {
			platformError(w, e)
		} else {
			jsonResponse(w, v)
		}
		return true
	}
	if path == "/api/network" && r.Method == "GET" {
		if !require(operator) {
			return true
		}
		v, e := s.NetworkSettings(ctx)
		if e != nil {
			platformError(w, e)
		} else {
			jsonResponse(w, v)
		}
		return true
	}
	if path == "/api/config/preview-output" && r.Method == "POST" {
		if !require(admin) {
			return true
		}
		var input OutputPreviewRequest
		if err = decodeRequest(w, r, &input, 16<<20); err == nil {
			var preview OutputPreview
			preview, err = s.PreviewOutput(ctx, input.Config, input.ID)
			if err == nil {
				jsonResponse(w, preview)
				return true
			}
		}
		platformError(w, err)
		return true
	}
	if path == "/api/config/preview" && r.Method == "POST" {
		if !require(admin) {
			return true
		}
		var c PlatformConfig
		if err = decodeRequest(w, r, &c, 16<<20); err == nil {
			var preview ConfigurationPreview
			preview, err = s.PreviewConfiguration(ctx, c)
			if err == nil {
				jsonResponse(w, preview)
				return true
			}
		}
		platformError(w, err)
		return true
	}
	if path == "/api/outbound-options" && r.Method == "GET" {
		if !require(admin) {
			return true
		}
		options, e := s.OutboundOptions(ctx)
		if e != nil {
			platformError(w, e)
		} else {
			jsonResponse(w, options)
		}
		return true
	}
	if path == "/api/server" && r.Method == "POST" {
		if !require(admin) {
			return true
		}
		var app App
		if err = decodeRequest(w, r, &app, 8<<20); err == nil {
			var v Object
			v, err = serverForApp(app)
			if err == nil {
				jsonResponse(w, v)
				return true
			}
		}
		platformError(w, err)
		return true
	}
	if path == "/api/state" && r.Method == "GET" {
		v, e := s.State(ctx, false)
		if e != nil {
			platformError(w, e)
		} else {
			jsonResponse(w, v)
		}
		return true
	}
	if path == "/api/config" {
		if !require(admin) {
			return true
		}
		if r.Method == "GET" {
			if !audit("configuration-secret-read", "") {
				return true
			}
			v, e := s.State(ctx, true)
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, v)
			}
			return true
		}
		if r.Method == "POST" {
			var v ConfigurationSave
			if err = decodeRequest(w, r, &v, 16<<20); err == nil {
				if !audit("configuration-save", "") {
					return true
				}
				var id int64
				submission, submitErr := taskSubmission(r, access.actor, v)
				if submitErr != nil {
					platformError(w, submitErr)
					return true
				}
				id, err = s.QueueApply(ctx, v.Config, v.Revision, submission)
				if err == nil {
					jsonResponse(w, QueuedTask{TaskID: id})
					return true
				}
			}
			platformError(w, err)
			return true
		}
	}
	if path == "/api/preset" && r.Method == "POST" {
		if !require(admin) {
			return true
		}
		var v InboundPreparation
		if err = decodeRequest(w, r, &v, maxJSONFile); err == nil {
			var node ManagedNode
			node, err = prepareInbound(s.Root, v.Config, v.Protocol, v.ID)
			if err == nil {
				jsonResponse(w, node)
				return true
			}
		}
		platformError(w, err)
		return true
	}
	if path == "/api/country" && r.Method == "POST" {
		if !require(admin) {
			return true
		}
		var v CountryPreparation
		if err = decodeRequest(w, r, &v, maxJSONFile); err == nil {
			var c PlatformConfig
			c, err = prepareCountry(s.Root, v.Config, v.Country)
			if err == nil {
				jsonResponse(w, c)
				return true
			}
		}
		platformError(w, err)
		return true
	}
	if path == "/api/gateway" && r.Method == "GET" {
		v, e := s.gateway(ctx, r.URL.Query().Get("resource"), nil)
		if e != nil {
			platformError(w, e)
		} else {
			jsonResponse(w, v)
		}
		return true
	}
	if path == "/api/certificates/prepare" && r.Method == "POST" {
		if !require(admin) {
			return true
		}
		var value CertificatePreparation
		if err = decodeRequest(w, r, &value, 4096); err == nil {
			var asset CertificateAsset
			asset, err = s.PrepareCertificate(ctx, value)
			if err == nil {
				jsonResponse(w, asset)
				return true
			}
		}
		platformError(w, err)
		return true
	}
	if path == "/api/certificates" {
		if r.Method == "GET" {
			v, e := s.CertificateStatus(ctx)
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, v)
			}
			return true
		}
		if r.Method == "POST" {
			if !require(admin) || !audit("certificate-import", "") {
				return true
			}
			var v CertificateImport
			if err = decodeRequest(w, r, &v, 384<<10); err == nil {
				var asset CertificateAsset
				asset, err = s.ImportCertificate(ctx, v)
				if err == nil {
					jsonResponse(w, asset)
					return true
				}
			}
			platformError(w, err)
			return true
		}
	}
	return false
}
