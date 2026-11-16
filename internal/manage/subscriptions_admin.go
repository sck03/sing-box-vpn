package manage

import (
	"bytes"
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

//go:embed subscriptions_admin.html
var subscriptionAdminPage []byte

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
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
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
		args := []string{"--root", root, "--core", core}
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
		var input io.Reader
		stage := ""
		defer func() {
			if stage != "" {
				_ = os.RemoveAll(stage)
			}
		}()
		staging := func() (string, error) {
			if stage == "" {
				var err error
				stage, err = os.MkdirTemp(root, ".admin-")
				if err != nil {
					return "", err
				}
			}
			return filepath.Join(stage, "input"), nil
		}
		binaryBackup := false
		var err error
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/config":
			args = append(args, "show")
		case r.Method == http.MethodGet && r.URL.Path == "/api/logs":
			args = append(args, "logs")
		case r.Method == http.MethodGet && r.URL.Path == "/api/clients":
			args = append(args, "clients")
		case r.Method == http.MethodGet && r.URL.Path == "/api/trash":
			args = append(args, "trash")
		case r.Method == http.MethodGet && r.URL.Path == "/api/export":
			args = append(args, "export", "--id", r.URL.Query().Get("id"))
		case r.Method == http.MethodPost && (r.URL.Path == "/api/preview" || r.URL.Path == "/api/apply"):
			var path string
			path, err = staging()
			if err == nil {
				err = AtomicBytes(path, request.Config, 0600)
			}
			args = append(args, strings.TrimPrefix(r.URL.Path, "/api/"), "--file", path)
		case r.Method == http.MethodPost && (r.URL.Path == "/api/backup" || r.URL.Path == "/api/restore-backup"):
			var path string
			path, err = staging()
			operation := strings.TrimPrefix(r.URL.Path, "/api/")
			binaryBackup = operation == "backup"
			if !binaryBackup && err == nil {
				err = AtomicBytes(path, request.Backup, 0600)
			}
			input = strings.NewReader(request.Password)
			args = append(args, operation, "--file", path)
			if request.Apply {
				args = append(args, "--apply")
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/action":
			switch request.Operation {
			case "refresh":
				args = append(args, "refresh")
			case "check":
				args = append(args, "check", "--id", request.ID)
				if request.ProbeURL != "" {
					args = append(args, "--probe-url", request.ProbeURL)
				}
			case "deliver", "revoke", "restore":
				args = append(args, request.Operation, "--id", request.ID)
			case "archive":
				args = append(args, "archive", "--kind", request.Kind, "--id", request.ID)
			case "grant":
				expires := request.Expires
				if expires == "" {
					expires = "0"
				}
				args = append(args, "grant", "--id", request.ID, "--outputs", request.Outputs, "--expires", expires, "--max-downloads", fmt.Sprint(request.MaxDownloads))
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
		var result bytes.Buffer
		if err := RunSubscriptions(r.Context(), args, input, &result); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if binaryBackup {
			data, err := readLimitedRegularFile(filepath.Join(stage, "input"), sub.MaxBody+52)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="subscriptions.sbvpn"`)
			_, _ = w.Write(data)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(result.Bytes())
	})
}
