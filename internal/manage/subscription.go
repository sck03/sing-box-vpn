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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type subscriptionSettings struct {
	Enabled       bool   `json:"enabled"`
	Port          int    `json:"port"`
	Token         string `json:"token"`
	Listen        string `json:"listen"`
	PublicBaseURL string `json:"public_base_url,omitempty"`
}

var subscriptionTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32,255}$`)

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
	if previous.Token == "" {
		previous.Token = randomSecret()
	}
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
	if err := ensureSubscriptionGrant(&c, previous.Token); err != nil {
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

func ensureSubscriptionGrant(c *sub.Config, token string) error {
	hash := sub.Digest([]byte(token))
	for _, g := range c.Grants {
		if g.TokenHash == hash && g.Revoked {
			return errors.New("默认授权已撤销，请先重置默认订阅令牌")
		}
		if g.TokenHash == hash && !g.Revoked {
			return nil
		}
	}
	ids := []string{}
	for _, o := range c.Outputs {
		if strings.HasPrefix(o.ID, "normal-") || strings.HasPrefix(o.ID, "vpngate-") {
			ids = append(ids, o.ID)
		}
	}
	// A listener can serve exclusively custom outputs through independent grants.
	// Do not grant those outputs implicitly or create an invalid empty grant.
	if len(ids) == 0 {
		return nil
	}
	c.Grants = slices.DeleteFunc(c.Grants, func(g sub.Grant) bool { return g.Revoked })
	c.Grants = append(c.Grants, sub.Grant{ID: "default-" + hash[:12], Name: "默认订阅", TokenHash: hash, Outputs: ids})
	c.Revision++
	return c.Validate()
}

func resetSubscriptionToken(root string) error {
	var settings subscriptionSettings
	found, err := ReadJSON(filepath.Join(root, "subscription.json"), &settings)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("请先启用订阅")
	}
	c, _, _, err := loadSubscriptions(root)
	if err != nil {
		return err
	}
	hash := sub.Digest([]byte(settings.Token))
	for i := range c.Grants {
		if c.Grants[i].TokenHash == hash {
			c.Grants[i].Revoked = true
		}
	}
	settings.Token = randomSecret()
	if err := ensureSubscriptionGrant(&c, settings.Token); err != nil {
		return err
	}
	files := fileBundle{}
	if err := files.json("subscription.json", settings); err != nil {
		return err
	}
	if err := files.json(subscriptionsConfig, c); err != nil {
		return err
	}
	return publish(root, files)
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
		if _, ok := findGrant(c, settings.Token, out.ID, time.Now()); !ok {
			continue
		}
		if _, err = fmt.Fprintf(output, "%s/s/%s/%s\n", subscriptionBase(settings, address), settings.Token, out.ID); err != nil {
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
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-child.Done():
				return
			case <-tick.C:
				_ = refreshSubscriptions(child, *root, filepath.Join(*root, "sing-box"), false)
				_ = deliverSubscriptions(child, *root, "", true, io.Discard)
			}
		}
	}()
	server := &http.Server{Handler: subscriptionHandler(*root), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10}
	err = serveSubscription(child, listener, server)
	cancel()
	<-done
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
