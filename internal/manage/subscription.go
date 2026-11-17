package manage

import (
	"context"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/httpserver"
)

type subscriptionSettings struct {
	Enabled       bool   `json:"enabled"`
	Port          int    `json:"port"`
	Listen        string `json:"listen"`
	PublicBaseURL string `json:"public_base_url,omitempty"`
}

var subscriptionTokenPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func subscriptionBase(settings subscriptionSettings, address string) string {
	if settings.PublicBaseURL != "" {
		return strings.TrimRight(settings.PublicBaseURL, "/")
	}
	return "http://" + net.JoinHostPort(address, strconv.Itoa(settings.Port))
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
	control, err := controlstore.Open(filepath.Join(*root, "control.db"))
	if err != nil {
		listener.Close()
		return err
	}
	defer control.Close()
	server := &http.Server{Handler: unifiedDownloadHandler(AccountService{Root: *root, Store: store, Control: control}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10}
	err = httpserver.Serve(child, listener, server, 32)
	cancel()
	return err
}
