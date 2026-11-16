package manage

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type subscriptionSettings struct {
	Enabled       bool   `json:"enabled"`
	Port          int    `json:"port"`
	Listen        string `json:"listen"`
	PublicBaseURL string `json:"public_base_url,omitempty"`
}

var subscriptionTokenPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func configureSubscription(root string, app App, settings VPNSettings, port int, options ...string) error {
	var previous subscriptionSettings
	if _, err := ReadJSON(filepath.Join(root, "subscription.json"), &previous); err != nil {
		return err
	}
	if port == 0 && previous.Port > 0 {
		port = previous.Port
	}
	if port < 0 || port > 65535 {
		return errors.New("订阅端口必须为 1-65535")
	}
	used := usedPorts(app, settings)
	ranges, err := usedPortRanges(app, settings)
	if err != nil {
		return err
	}
	if port == 0 {
		value, err := AvailablePort(used, ranges...)
		if err != nil {
			return err
		}
		port = value
	} else {
		if used[port] || portInRanges(port, ranges) {
			return errors.New("订阅端口已被节点使用")
		}
		if !previous.Enabled || port != previous.Port {
			if err := probePort(port); err != nil {
				return err
			}
		}
	}
	previous.Enabled, previous.Port = true, port
	if previous.Listen == "" {
		previous.Listen = "::"
	}
	if len(options) > 0 && options[0] != "" {
		previous.Listen = options[0]
	}
	if len(options) > 1 && options[1] != "" {
		previous.PublicBaseURL = strings.TrimRight(options[1], "/")
		if options[1] == "-" {
			previous.PublicBaseURL = ""
		}
	}
	if net.ParseIP(previous.Listen) == nil {
		return errors.New("订阅监听地址必须为 IP")
	}
	if previous.PublicBaseURL != "" {
		if err := sub.HTTPURL(previous.PublicBaseURL); err != nil {
			return err
		}
		u, _ := url.Parse(previous.PublicBaseURL)
		if u.RawQuery != "" || u.RawPath != "" {
			return errors.New("公网地址不能包含查询参数或转义路径")
		}
	}
	c, _, _, err := loadSubscriptions(root)
	if err != nil {
		return err
	}
	files := fileBundle{}
	if err := files.json("subscription.json", previous); err != nil {
		return err
	}
	if err := files.json(subscriptionsConfig, c); err != nil {
		return err
	}
	return publish(root, files)
}

func resetSubscriptionToken(root string) error {
	return errors.New("default sharing tokens were removed; revoke and create explicit download grants")
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
	c, _, _, err := loadSubscriptions(root)
	if err != nil {
		return err
	}
	count := 0
	for _, out := range c.Outputs {
		if !strings.HasPrefix(out.ID, scope+"-") {
			continue
		}
		if _, err = fmt.Fprintf(output, "输出 %s：请从面板创建下载授权\n", out.ID); err != nil {
			return err
		}
		count++
	}
	if count == 0 {
		_, err = fmt.Fprintln(output, "当前范围没有有效的默认分享，请使用 subscriptions grant 创建独立分享")
		return err
	}
	return nil
}

func subscriptionBase(settings subscriptionSettings, address string) string {
	if settings.PublicBaseURL != "" {
		return strings.TrimRight(settings.PublicBaseURL, "/")
	}
	return "http://" + net.JoinHostPort(address, strconv.Itoa(settings.Port))
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
	if !found || !settings.Enabled {
		return errors.New("订阅监听服务未启用")
	}
	if net.ParseIP(settings.Listen) == nil || settings.Port < 1 || settings.Port > 65535 {
		return errors.New("订阅监听地址或端口错误")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(settings.Listen, strconv.Itoa(settings.Port)))
	if err != nil {
		return err
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	store, err := accounting.Open(filepath.Join(*root, "control.db"))
	if err != nil {
		listener.Close()
		return err
	}
	defer store.Close()
	server := &http.Server{Handler: unifiedDownloadHandler(*root, store), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10}
	err = serveSubscription(child, listener, server)
	cancel()
	return err
}

func serveSubscription(ctx context.Context, listener net.Listener, server *http.Server) error {
	limited := &limitedListener{Listener: listener, slots: make(chan struct{}, 32), done: make(chan struct{})}
	defer limited.Close()
	defer server.Close()
	stop := context.AfterFunc(ctx, func() { server.Close() })
	defer stop()
	err := server.Serve(limited)
	if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return err
}
