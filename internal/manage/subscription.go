package manage

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type subscriptionSettings struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
	Token   string `json:"token"`
	Listen  string `json:"listen"`
}

var subscriptionTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32,255}$`)

func configureSubscription(root string, app App, settings VPNSettings, port int) error {
	if port < 0 || port > 65535 {
		return errors.New("订阅端口必须为 1-65535")
	}
	used := usedPorts(app, settings)
	if port == 0 {
		value, err := AvailablePort(used)
		if err != nil {
			return err
		}
		port = value
	} else {
		if used[port] {
			return errors.New("订阅端口已被节点使用")
		}
		if err := probePort(port); err != nil {
			return err
		}
	}
	return AtomicJSON(filepath.Join(root, "subscription.json"), subscriptionSettings{true, port, randomSecret(), "0.0.0.0"})
}
func disableSubscription(root string) error {
	var settings subscriptionSettings
	found, err := ReadJSON(filepath.Join(root, "subscription.json"), &settings)
	if err != nil || !found {
		return err
	}
	settings.Enabled = false
	return AtomicJSON(filepath.Join(root, "subscription.json"), settings)
}
func subscriptionURLs(root, address, scope string, output io.Writer) error {
	if _, err := exportDirectory(root, scope); err != nil {
		return err
	}
	var settings subscriptionSettings
	found, err := ReadJSON(filepath.Join(root, "subscription.json"), &settings)
	if err != nil {
		return err
	}
	if !found || !settings.Enabled {
		_, err = fmt.Fprintln(output, "本地订阅未启用，请通过菜单 3 → 8 配置")
		return err
	}
	if strings.Contains(address, ":") {
		address = "[" + address + "]"
	}
	prefix := ""
	if scope == vpngateScope {
		prefix = "vpngate/"
	}
	for _, name := range []string{"sbox.json", "clmi.yaml", "jhsub.txt"} {
		if _, err = fmt.Fprintf(output, "http://%s:%d/%s/%s%s\n", address, settings.Port, settings.Token, prefix, name); err != nil {
			return err
		}
	}
	return nil
}

type limitedListener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
}
type limitedConn struct {
	net.Conn
	release func()
	once    sync.Once
}

func (c *limitedConn) Close() error { err := c.Conn.Close(); c.once.Do(c.release); return err }
func (l *limitedListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &limitedConn{Conn: c, release: func() { <-l.slots }}, nil
}
func (l *limitedListener) Close() error {
	var err error
	l.once.Do(func() { close(l.done); err = l.Listener.Close() })
	return err
}

func subscriptionHandler(root, token string) http.Handler {
	type exportedFile struct{ path, mime string }
	files := map[string]exportedFile{}
	for _, prefix := range []string{"", "vpngate/"} {
		files[prefix+"sbox.json"] = exportedFile{prefix + "sbox.json", "application/json"}
		files[prefix+"clmi.yaml"] = exportedFile{prefix + "clmi.yaml", "application/yaml"}
		files[prefix+"jhsub.txt"] = exportedFile{prefix + "jhsub-base64.txt", "text/plain"}
		files[prefix+"links.txt"] = exportedFile{prefix + "jhsub.txt", "text/plain"}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", 405)
			return
		}
		credential, name, valid := strings.Cut(strings.Trim(r.URL.Path, "/"), "/")
		if !valid || subtle.ConstantTimeCompare([]byte(credential), []byte(token)) != 1 {
			http.NotFound(w, r)
			return
		}
		item, exists := files[name]
		if !exists {
			http.NotFound(w, r)
			return
		}
		file, err := os.Open(filepath.Join(root, item.path))
		if err != nil {
			http.Error(w, "subscription unavailable", 503)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.Error(w, "subscription unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", item.mime+"; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, item.path, info.ModTime(), file)
	})
}

func RunSubscription(ctx context.Context, args []string, output io.Writer) error {
	f := flag.NewFlagSet("subscription", flag.ContinueOnError)
	f.SetOutput(output)
	root := f.String("root", "/etc/s-box", "configuration directory")
	if err := f.Parse(args); err != nil {
		return err
	}
	settings := subscriptionSettings{Listen: "0.0.0.0"}
	found, err := ReadJSON(filepath.Join(*root, "subscription.json"), &settings)
	if err != nil {
		return err
	}
	if !found || !settings.Enabled || !subscriptionTokenPattern.MatchString(settings.Token) {
		return errors.New("订阅服务未配置强路径令牌")
	}
	if net.ParseIP(settings.Listen) == nil || settings.Port < 1 || settings.Port > 65535 {
		return errors.New("订阅监听地址或端口错误")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(settings.Listen, strconv.Itoa(settings.Port)))
	if err != nil {
		return err
	}
	limited := &limitedListener{Listener: listener, slots: make(chan struct{}, 32), done: make(chan struct{})}
	defer limited.Close()
	server := &http.Server{Handler: subscriptionHandler(*root, settings.Token), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			server.Close()
		case <-done:
		}
	}()
	err = server.Serve(limited)
	if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return err
}
