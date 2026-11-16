package manage

import (
	"bytes"
	"context"
	"crypto/tls"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	qrcode "github.com/skip2/go-qrcode"
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
	base, _ := url.Parse(options.PublicURL)
	secure := base != nil && base.Scheme == "https"
	cookieName := "sbvpn"
	if secure {
		cookieName = "__Host-sbvpn"
	}
	proxy := []netip.Prefix{}
	for _, v := range options.TrustedProxy {
		if p, err := netip.ParsePrefix(v); err == nil {
			proxy = append(proxy, p)
		}
	}
	loginSlots := make(chan struct{}, 2)
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
		if len(proxy) > 0 {
			ip, err := netip.ParseAddr(peer)
			if err != nil || !slices.ContainsFunc(proxy, func(p netip.Prefix) bool { return p.Contains(ip) }) || r.Header.Get("X-Forwarded-Proto") != "https" {
				http.Error(w, "untrusted proxy", 403)
				return
			}
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != options.PublicURL {
			http.Error(w, "cross-origin request rejected", 403)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "cross-site request rejected", 403)
			return
		}
		path := r.URL.Path
		if (path == "/" || path == "/user" || strings.HasPrefix(path, "/panel/")) && r.Method == "GET" {
			name := "panel/index.html"
			if path == "/user" {
				name = "panel/user.html"
			} else if strings.HasPrefix(path, "/panel/") {
				name = strings.TrimPrefix(path, "/")
			}
			data, err := panelAssets.ReadFile(name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			mime := "text/html; charset=utf-8"
			if strings.HasSuffix(name, ".js") {
				mime = "text/javascript; charset=utf-8"
			}
			if strings.HasSuffix(name, ".css") {
				mime = "text/css; charset=utf-8"
			}
			w.Header().Set("Content-Type", mime)
			_, _ = w.Write(data)
			return
		}
		if strings.HasPrefix(path, "/u/") {
			serveAccountDownload(s.Root, w, r, s.Accounts)
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
		if path == "/api/inbounds/impact" && r.Method == "GET" {
			if !require(admin) {
				return
			}
			v, e := s.InboundImpact(ctx, r.URL.Query().Get("scope"), r.URL.Query().Get("id"))
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, v)
			}
			return
		}
		if path == "/api/network" && r.Method == "GET" {
			if !require(operator) {
				return
			}
			v, e := s.NetworkSettings(ctx)
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, v)
			}
			return
		}
		if path == "/api/config/preview" && r.Method == "POST" {
			if !require(admin) {
				return
			}
			var c PlatformConfig
			if err = decodeRequest(w, r, &c, 16<<20); err == nil {
				var files fileBundle
				files, err = s.build(ctx, c)
				if err == nil {
					var preview any
					if err = json.Unmarshal(files["sb.json"], &preview); err == nil {
						redactPlatform(preview)
						jsonResponse(w, Object{"valid": true, "ordinary_server": preview})
						return
					}
				}
			}
			platformError(w, err)
			return
		}
		if path == "/api/server" && r.Method == "POST" {
			if !require(admin) {
				return
			}
			var app App
			if err = decodeRequest(w, r, &app, 8<<20); err == nil {
				var v Object
				v, err = serverForApp(app)
				if err == nil {
					jsonResponse(w, v)
					return
				}
			}
			platformError(w, err)
			return
		}
		if path == "/api/events" && r.Method == "GET" {
			s.streamOverview(w, r, cookie.Value)
			return
		}
		if path == "/api/qr" && r.Method == "POST" {
			if !require(admin) {
				return
			}
			var v struct {
				Content string `json:"content"`
			}
			if err = decodeRequest(w, r, &v, 4096); err != nil {
				platformError(w, err)
				return
			}
			if len(v.Content) > 2048 {
				http.Error(w, "QR content exceeds 2048 bytes", 400)
				return
			}
			png, e := qrcode.Encode(v.Content, qrcode.Medium, 320)
			if e != nil {
				platformError(w, e)
				return
			}
			jsonResponse(w, Object{"image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)})
			return
		}
		if path == "/api/accounts/export" && r.Method == "POST" {
			if !require(admin) || !audit("account-export", "") {
				return
			}
			var v application.AccountCommand
			if err = decodeRequest(w, r, &v, 8192); err != nil {
				platformError(w, err)
				return
			}
			v.Operation = "export"
			artifact, e := (AccountService{Root: s.Root, Store: s.Accounts}).Export(ctx, v)
			if e != nil {
				platformError(w, e)
				return
			}
			jsonResponse(w, Object{"content": string(artifact.Data)})
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
		if path == "/api/state" && r.Method == "GET" {
			v, e := s.State(ctx, false)
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, v)
			}
			return
		}
		if path == "/api/config" {
			if !require(admin) {
				return
			}
			if r.Method == "GET" {
				if !audit("configuration-secret-read", "") {
					return
				}
				v, e := s.State(ctx, true)
				if e != nil {
					platformError(w, e)
				} else {
					jsonResponse(w, v)
				}
				return
			}
			if r.Method == "POST" {
				var v struct {
					Revision int64          `json:"revision"`
					Config   PlatformConfig `json:"config"`
				}
				if err = decodeRequest(w, r, &v, 16<<20); err == nil {
					if !audit("configuration-save", "") {
						return
					}
					var id int64
					id, err = s.QueueApply(ctx, v.Config, v.Revision, session.ID)
					if err == nil {
						jsonResponse(w, Object{"task_id": id})
						return
					}
				}
				platformError(w, err)
				return
			}
		}
		if path == "/api/overview" && r.Method == "GET" {
			state, e := s.Store.State(ctx)
			if e != nil {
				platformError(w, e)
				return
			}
			services, _ := s.ServiceStatus(ctx)
			runtimes, e := s.Store.Runtimes(ctx)
			if e != nil {
				platformError(w, e)
				return
			}
			tasks, e := s.Store.TaskSummaries(ctx)
			if e != nil {
				platformError(w, e)
				return
			}
			for i := range tasks {
				tasks[i].Result = nil
				if tasks[i].Error != "" {
					tasks[i].Error = "任务失败，请查看任务详情"
				}
			}
			jsonResponse(w, Object{"revision": state.Revision, "applied_revision": state.AppliedRevision, "services": services, "runtimes": runtimes, "tasks": tasks})
			return
		}
		if path == "/api/tasks/detail" && r.Method == "GET" {
			if !require(operator) {
				return
			}
			id, e := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
			if e != nil || id < 1 {
				http.Error(w, "invalid task ID", 400)
				return
			}
			v, e := s.Store.Task(ctx, id)
			if e != nil {
				platformError(w, e)
				return
			}
			if !require(admin || v.Kind == "service" || v.Kind == "gateway" || (v.Kind == "apply" && v.Actor == session.ID)) {
				return
			}
			jsonResponse(w, v)
			return
		}
		if path == "/api/tasks" && r.Method == "POST" {
			var a PlatformAction
			if err = decodeRequest(w, r, &a, 1<<20); err != nil {
				platformError(w, err)
				return
			}
			if !require(admin || (operator && (a.Operation == "service" || a.Operation == "gateway"))) {
				return
			}
			if !audit(a.Operation, a.ID) {
				return
			}
			id, e := s.Queue(ctx, a, session.ID)
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, Object{"task_id": id})
			}
			return
		}
		if path == "/api/tasks/cancel" && r.Method == "POST" {
			if !require(admin) {
				return
			}
			var v struct {
				ID int64 `json:"id"`
			}
			if err = decodeRequest(w, r, &v, 1024); err == nil {
				err = s.Store.Cancel(ctx, v.ID)
			}
			if err != nil {
				platformError(w, err)
			} else {
				jsonResponse(w, Object{"ok": true})
			}
			return
		}
		if path == "/api/accounts" {
			if r.Method == "GET" {
				var accounts []accounting.Usage
				accounts, err = (AccountService{Root: s.Root, Store: s.Accounts}).List(ctx)
				if err == nil {
					jsonResponse(w, accounts)
					return
				}
			} else if r.Method == "POST" {
				if !require(admin) {
					return
				}
				var v domainAccountRequest
				if err = decodeRequest(w, r, &v, 64<<10); err == nil {
					if !audit("account-save", v.Account.ID) {
						return
					}
					err = s.Accounts.SaveAccount(ctx, v.Account)
					if err == nil {
						jsonResponse(w, Object{"ok": true})
						return
					}
				}
			}
			if err != nil {
				platformError(w, err)
				return
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
				return
			}
			if r.Method == "POST" {
				if !require(admin) {
					return
				}
				var v accounting.Subscriber
				if err = decodeRequest(w, r, &v, 4096); err == nil {
					if !audit("subscriber-save", v.ID) {
						return
					}
					err = s.Accounts.SaveSubscriber(ctx, v)
				}
				if err != nil {
					platformError(w, err)
				} else {
					jsonResponse(w, Object{"ok": true})
				}
				return
			}
		}
		if path == "/api/credentials" && r.Method == "GET" {
			if !require(admin) {
				return
			}
			var credentials []CredentialSummary
			credentials, err = (AccountService{Root: s.Root, Store: s.Accounts}).Credentials(ctx, r.URL.Query().Get("id"), r.URL.Query().Get("scope"))
			if err != nil {
				platformError(w, err)
			} else {
				jsonResponse(w, credentials)
			}
			return
		}
		if path == "/api/history" && r.Method == "GET" {
			v, e := s.Accounts.History(ctx, r.URL.Query().Get("id"))
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, v)
			}
			return
		}
		if path == "/api/grants" {
			if !require(admin) {
				return
			}
			if r.Method == "GET" {
				v, e := s.Accounts.Grants(ctx)
				if e != nil {
					platformError(w, e)
				} else {
					jsonResponse(w, v)
				}
				return
			}
			if r.Method == "POST" {
				var g accounting.Grant
				if err = decodeRequest(w, r, &g, 8192); err == nil {
					if !audit("download-grant", g.ID) {
						return
					}
					var token string
					var origin string
					origin, err = s.downloadOrigin(ctx, options.PublicURL)
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
						return
					}
				}
				platformError(w, err)
				return
			}
		}
		if path == "/api/grants/revoke" && r.Method == "POST" {
			if !require(admin) {
				return
			}
			var v struct {
				ID string `json:"id"`
			}
			if err = decodeRequest(w, r, &v, 1024); err == nil {
				if !audit("grant-revoke", v.ID) {
					return
				}
				err = s.Accounts.RevokeGrant(ctx, v.ID)
			}
			if err != nil {
				platformError(w, err)
			} else {
				jsonResponse(w, Object{"ok": true})
			}
			return
		}
		if path == "/api/preset" && r.Method == "POST" {
			if !require(admin) {
				return
			}
			var v struct {
				ID       string `json:"id"`
				Protocol string `json:"protocol"`
			}
			if err = decodeRequest(w, r, &v, 1024); err == nil {
				var node ManagedNode
				node, err = s.Preset(ctx, v.Protocol, v.ID)
				if err == nil {
					jsonResponse(w, node)
					return
				}
			}
			platformError(w, err)
			return
		}
		if path == "/api/country" && r.Method == "POST" {
			if !require(admin) {
				return
			}
			var v struct {
				Country string `json:"country"`
			}
			if err = decodeRequest(w, r, &v, 1024); err == nil {
				var c PlatformConfig
				c, err = s.PrepareCountry(ctx, v.Country)
				if err == nil {
					jsonResponse(w, c)
					return
				}
			}
			platformError(w, err)
			return
		}
		if path == "/api/gateway" && r.Method == "GET" {
			v, e := s.gateway(ctx, r.URL.Query().Get("resource"), nil)
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, v)
			}
			return
		}
		if path == "/api/certificates" {
			if r.Method == "GET" {
				v, e := s.CertificateStatus(ctx)
				if e != nil {
					platformError(w, e)
				} else {
					jsonResponse(w, v)
				}
				return
			}
			if r.Method == "POST" {
				if !require(admin) || !audit("certificate-import", "") {
					return
				}
				var v CertificateImport
				if err = decodeRequest(w, r, &v, 384<<10); err == nil {
					var asset CertificateAsset
					asset, err = s.ImportCertificate(v)
					if err == nil {
						jsonResponse(w, asset)
						return
					}
				}
				platformError(w, err)
				return
			}
		}
		if path == "/api/logs" && r.Method == "GET" {
			if !require(operator) || !audit("service-logs", r.URL.Query().Get("service")) {
				return
			}
			v, e := s.Logs(ctx, r.URL.Query().Get("service"))
			if e != nil {
				platformError(w, e)
			} else {
				jsonResponse(w, Object{"log": v})
			}
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
		if path == "/api/subscriptions/preview" && r.Method == "POST" {
			if !require(admin) {
				return
			}
			var v sub.Config
			if err = decodeRequest(w, r, &v, 8<<20); err == nil {
				data, _ := json.Marshal(v)
				var out bytes.Buffer
				err = (SubscriptionService{Root: s.Root, Core: s.Core}).Execute(ctx, application.SubscriptionCommand{Operation: "preview", Config: data}, &out)
				if err == nil {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(out.Bytes())
					return
				}
			}
			platformError(w, err)
			return
		}
		http.NotFound(w, r)
	})
}

func (s *PlatformService) Serve(ctx context.Context, options PanelOptions, out io.Writer) error {
	host, _, err := net.SplitHostPort(options.Listen)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return errors.New("panel listen address must be an IP")
	}
	if options.Certificate == "" && len(options.TrustedProxy) == 0 && !ip.IsLoopback() {
		return errors.New("public panel requires TLS or an explicitly trusted TLS reverse proxy")
	}
	for _, cidr := range options.TrustedProxy {
		if _, err = netip.ParsePrefix(cidr); err != nil {
			return err
		}
	}
	listener, err := net.Listen("tcp", options.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	if options.PublicURL == "" {
		options.PublicURL = "http://" + listener.Addr().String()
		if options.Certificate != "" {
			options.PublicURL = "https://" + listener.Addr().String()
		}
	}
	base, err := url.Parse(options.PublicURL)
	if err != nil || base.Host == "" || base.Path != "" || base.RawQuery != "" || base.Fragment != "" || base.User != nil || !slices.Contains([]string{"http", "https"}, base.Scheme) {
		return errors.New("public URL must be an HTTP(S) origin without a path")
	}
	if (len(options.TrustedProxy) > 0 || options.Certificate != "") && base.Scheme != "https" {
		return errors.New("TLS panel public URL must use https")
	}
	if options.Certificate != "" {
		pair, e := tls.LoadX509KeyPair(options.Certificate, options.Key)
		if e != nil {
			return e
		}
		listener = tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12})
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	worker := make(chan error, 1)
	go func() { worker <- s.Run(child); cancel() }()
	server := &http.Server{Handler: s.Handler(options), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 3 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	fmt.Fprintln(out, "管理面板："+options.PublicURL)
	err = serveSubscription(child, listener, server)
	cancel()
	return errors.Join(err, <-worker)
}

type domainAccountRequest struct {
	Account domain.Account `json:"account"`
}
