package manage

import (
	"bytes"
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
)

//go:embed subscriptions_admin.html
var subscriptionAdminPage []byte

//go:embed web/*.js
var subscriptionAdminAssets embed.FS

func serveSubscriptionAdmin(ctx context.Context, root, core, address string, output io.Writer) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("administration must listen on a loopback IP; use an SSH tunnel for remote access")
	}
	var token string
	if err := withLock(root, func() error {
		path := filepath.Join(root, "subscriptions/admin.json")
		found, err := ReadJSON(path, &token)
		if err != nil {
			return err
		}
		if !found {
			token = randomSecret()
			return AtomicJSON(path, token)
		}
		if !subscriptionTokenPattern.MatchString(token) {
			return errors.New("invalid administration token file")
		}
		return nil
	}); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "管理地址：http://%s\n管理令牌：%s\n", listener.Addr(), token); err != nil {
		listener.Close()
		return err
	}
	server := &http.Server{Handler: subscriptionAdminHandler(root, core, token), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 10 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	return serveSubscription(ctx, listener, server)
}

type adminRequest struct {
	Operation    string          `json:"operation"`
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Outputs      string          `json:"outputs"`
	Expires      string          `json:"expires"`
	MaxDownloads uint64          `json:"max_downloads"`
	ProbeURL     string          `json:"probe_url"`
	Config       json.RawMessage `json:"config"`
	Password     string          `json:"password"`
	Backup       []byte          `json:"backup"`
	Apply        bool            `json:"apply"`
}

func subscriptionAdminHandler(root, core, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		if r.Method == http.MethodGet && (r.URL.Path == "/web/admin.js" || r.URL.Path == "/web/api.js" || r.URL.Path == "/web/accounts.js") {
			data, err := subscriptionAdminAssets.ReadFile(strings.TrimPrefix(r.URL.Path, "/"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(data)
			return
		}
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(subscriptionAdminPage)
			return
		}
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Never allow a cross-origin page to operate on this local administrator.
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
			http.Error(w, "cross-origin request rejected", 403)
			return
		}
		if r.URL.Path == "/api/accounts" {
			serveAccountCommand(root, w, r)
			return
		}
		service := SubscriptionService{Root: root, Core: core}
		var request adminRequest
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&request); err != nil {
				http.Error(w, "invalid request JSON", 400)
				return
			}
			if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
				http.Error(w, "expected one JSON request", 400)
				return
			}
		} else if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", 405)
			return
		}
		command := application.SubscriptionCommand{ID: request.ID, Kind: request.Kind, Outputs: strings.Split(request.Outputs, ","), MaxDownloads: request.MaxDownloads, Config: request.Config, ProbeURL: request.ProbeURL}
		binaryBackup := false
		var err error
		var result bytes.Buffer
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/config":
			command.Operation = "show"
		case r.Method == http.MethodGet && r.URL.Path == "/api/logs":
			command.Operation = "logs"
		case r.Method == http.MethodGet && r.URL.Path == "/api/clients":
			command.Operation = "clients"
		case r.Method == http.MethodGet && r.URL.Path == "/api/trash":
			command.Operation = "trash"
		case r.Method == http.MethodGet && r.URL.Path == "/api/export":
			command.Operation, command.ID = "export", r.URL.Query().Get("id")
		case r.Method == http.MethodPost && (r.URL.Path == "/api/preview" || r.URL.Path == "/api/apply"):
			command.Operation = strings.TrimPrefix(r.URL.Path, "/api/")
		case r.Method == http.MethodPost && (r.URL.Path == "/api/backup" || r.URL.Path == "/api/restore-backup"):
			binaryBackup = r.URL.Path == "/api/backup"
			password := []byte(request.Password)
			defer clear(password)
			if binaryBackup {
				err = service.Backup(r.Context(), password, &result)
			} else {
				err = service.RestoreBackup(r.Context(), request.Backup, password, request.Apply, &result)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/action":
			switch request.Operation {
			case "refresh":
				command.Operation = "refresh"
			case "check":
				command.Operation = "check"
				if command.ProbeURL == "" {
					command.ProbeURL = "https://www.gstatic.com/generate_204"
				}
			case "deliver", "revoke", "restore":
				command.Operation = request.Operation
			case "archive":
				command.Operation = "archive"
			case "grant":
				expires := request.Expires
				if expires == "" {
					expires = "0"
				}
				command.Operation = "grant"
				command.Expires, err = time.ParseDuration(expires)
			default:
				err = errors.New("unknown administration action")
			}
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if command.Operation != "" {
			err = service.Execute(r.Context(), command, &result)
		}
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, application.ErrConflict) {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		if binaryBackup {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="subscriptions.sbvpn"`)
			_, _ = w.Write(result.Bytes())
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(result.Bytes())
	})
}

func serveAccountCommand(root string, w http.ResponseWriter, r *http.Request) {
	command := application.AccountCommand{Operation: "list"}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&command); err != nil {
			http.Error(w, "invalid account command", 400)
			return
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			http.Error(w, "expected one command", 400)
			return
		}
	} else if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	var result bytes.Buffer
	err := (AccountService{Root: root}).Execute(r.Context(), command, &result)
	if err != nil {
		code := 400
		if errors.Is(err, accounting.ErrConflict) {
			code = 409
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(result.Bytes())
}
