package manage

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/httpserver"
	"github.com/sck03/sing-box-vpn/internal/publicaccess"
)

//go:embed panel/*
var panelAssets embed.FS

type PanelOptions struct {
	Listen       string   `json:"listen"`
	PublicURL    string   `json:"public_url"`
	Certificate  string   `json:"certificate"`
	Key          string   `json:"key"`
	TrustedProxy []string `json:"trusted_proxy"`
}

func decodeRequest(w http.ResponseWriter, r *http.Request, target any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid JSON request")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("expected one JSON request")
	}
	return nil
}
func jsonResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}
func platformError(w http.ResponseWriter, err error) {
	code := 400
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		code = http.StatusServiceUnavailable
		w.Header().Set("Retry-After", "1")
	}
	if errors.Is(err, controlstore.ErrConflict) || errors.Is(err, accounting.ErrConflict) {
		code = 409
	}
	if errors.Is(err, adminauth.ErrUnauthorized) {
		code = 401
	}
	if errors.Is(err, adminauth.ErrLimited) {
		code = 429
		w.Header().Set("Retry-After", "900")
	}
	http.Error(w, err.Error(), code)
}

func (s *PlatformService) Handler(options PanelOptions) http.Handler {
	s.panelMu.Lock()
	s.activePanel = &options
	s.panelMu.Unlock()
	base, _ := url.Parse(options.PublicURL)
	secure := base != nil && base.Scheme == "https"
	cookieName := "sbvpn"
	if secure {
		cookieName = "__Host-sbvpn"
	}
	proxy, _ := publicaccess.NewProxyPolicy(options.TrustedProxy)
	loginSlots := make(chan struct{}, 2)
	requests, streams := make(httpserver.Gate, 32), make(httpserver.Gate, 32)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if secure {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		peer, _, _ := net.SplitHostPort(r.RemoteAddr)
		if base == nil || r.Host != base.Host {
			http.Error(w, "unexpected Host", 400)
			return
		}
		if r.URL.Path == "/healthz" && r.Method == "GET" && localPanelPeer(r, peer) {
			if !s.workerReady.Load() {
				http.Error(w, "worker is starting", http.StatusServiceUnavailable)
			} else {
				jsonResponse(w, Object{"ready": true})
			}
			return
		}
		// Local readiness performs no database or expensive work and remains
		// available while application requests occupy their concurrency budget.
		gate := requests
		if r.URL.Path == "/api/events" {
			gate = streams
		}
		release, ok := gate.Enter(w)
		if !ok {
			return
		}
		defer release()
		if len(proxy) > 0 {
			var err error
			peer, err = proxy.Peer(r)
			if err != nil {
				http.Error(w, err.Error(), 403)
				return
			}
		}
		path := r.URL.Path
		publicRead := (r.Method == "GET" || r.Method == "HEAD") && (path == "/" || path == "/user" || path == "/api/capabilities" || strings.HasPrefix(path, "/panel/") || strings.HasPrefix(path, "/u/") || strings.HasPrefix(path, "/s/"))
		if origin := r.Header.Get("Origin"); !publicRead && origin != "" && origin != options.PublicURL {
			http.Error(w, "cross-origin request rejected", 403)
			return
		}
		if !publicRead && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "cross-site request rejected", 403)
			return
		}
		if path == "/api/capabilities" && r.Method == "GET" {
			jsonResponse(w, panelCapabilities())
			return
		}
		if (path == "/" || path == "/user" || strings.HasPrefix(path, "/panel/")) && r.Method == "GET" {
			name := "panel/index.html"
			if path == "/user" {
				name = "panel/user.html"
			} else if strings.HasPrefix(path, "/panel/") {
				name = strings.TrimPrefix(path, "/")
			}
			servePanelAsset(w, r, name)
			return
		}
		if strings.HasPrefix(path, "/u/") || strings.HasPrefix(path, "/s/") {
			unifiedDownloadHandler(s.accountService()).ServeHTTP(w, r)
			return
		}
		if path == "/api/auth/login" && r.Method == "POST" {
			select {
			case loginSlots <- struct{}{}:
				defer func() { <-loginSlots }()
			default:
				http.Error(w, "login service busy", 429)
				return
			}
			var v struct {
				Username string `json:"username"`
				Password string `json:"password"`
				Code     string `json:"code"`
			}
			if err := decodeRequest(w, r, &v, 4096); err != nil {
				platformError(w, err)
				return
			}
			session, err := s.Auth.Login(r.Context(), v.Username, v.Password, v.Code, peer)
			if err != nil {
				platformError(w, err)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: session.Token, Path: "/", Secure: secure, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: session.Expires})
			jsonResponse(w, session)
			return
		}
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			platformError(w, adminauth.ErrUnauthorized)
			return
		}
		write := r.Method != "GET" && r.Method != "HEAD"
		session, err := s.Auth.Authenticate(r.Context(), cookie.Value, r.Header.Get("X-CSRF-Token"), write)
		if err != nil {
			platformError(w, err)
			return
		}
		admin := session.Role == "admin"
		operator := admin || session.Role == "operator"
		require := func(allowed bool) bool {
			if !allowed {
				http.Error(w, "permission denied", 403)
			}
			return allowed
		}
		audit := func(action, target string) bool {
			if e := s.Auth.Audit(r.Context(), session.ID, action, target, "requested"); e != nil {
				http.Error(w, "audit storage unavailable", 503)
				return false
			}
			return true
		}
		ctx := r.Context()
		access := panelAccess{actor: session.ID, publicURL: options.PublicURL, admin: admin, operator: operator, require: require, audit: audit}
		if s.servePanelRestart(w, r, access, options) || s.servePanelSettings(w, r, access, options) {
			return
		}
		if s.serveAccounts(w, r, access) || s.serveGrants(w, r, access) || s.serveConfiguration(w, r, access) || s.serveTasks(w, r, access) {
			return
		}
		if path == "/api/events" && r.Method == "GET" {
			s.streamOverview(w, r, cookie.Value)
			return
		}
		if path == "/api/auth/session" && r.Method == "GET" {
			session.CSRF = adminauth.SessionCSRF(cookie.Value)
			jsonResponse(w, session)
			return
		}
		if path == "/api/auth/logout" && r.Method == "POST" {
			err = s.Auth.Logout(ctx, cookie.Value)
			http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", Secure: secure, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
			if err != nil {
				platformError(w, err)
			} else {
				jsonResponse(w, Object{"ok": true})
			}
			return
		}
		if path == "/api/overview" && r.Method == "GET" {
			data, e := s.overviewSnapshot(ctx)
			if e != nil {
				platformError(w, e)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
			return
		}
		if strings.HasPrefix(path, "/api/admin") || strings.HasPrefix(path, "/api/mfa") {
			if !require(admin) {
				return
			}
			s.serveAdministrators(w, r, session.ID)
			return
		}
		if path == "/api/backup" || path == "/api/restore" {
			if !require(admin) || !audit(strings.TrimPrefix(path, "/api/"), "") {
				return
			}
			s.servePlatformBackup(w, r, session.ID)
			return
		}
		http.NotFound(w, r)
	})
}

func (s *PlatformService) Serve(ctx context.Context, options PanelOptions, out io.Writer) error {
	if err := options.validate(s.NoSystem); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", options.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	_, requestedPort, _ := net.SplitHostPort(options.Listen)
	if requestedPort == "0" {
		options.Listen = listener.Addr().String()
	}
	if err := s.validatePanelListener(ctx, options); err != nil {
		return err
	}
	options.PublicURL = options.origin()
	if options.Certificate != "" {
		listener = tls.NewListener(listener, publicaccess.Options(options).TLS())
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	worker := make(chan error, 1)
	go func() { worker <- s.Run(child); cancel() }()
	server := &http.Server{Handler: s.Handler(options), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 3 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	fmt.Fprintln(out, "管理面板："+options.PublicURL)
	err = httpserver.Serve(child, listener, server, 128)
	cancel()
	return errors.Join(err, <-worker)
}

// panelAccess is constructed only after the shared origin, session and CSRF checks.
// Feature handlers retain explicit role and durable audit checks for each action.
type panelAccess struct {
	actor, publicURL string
	admin, operator  bool
	require          func(bool) bool
	audit            func(string, string) bool
}
