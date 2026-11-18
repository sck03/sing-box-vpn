package manage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/controlstore"
)

func defaultPanelOptions() PanelOptions { return PanelOptions{Listen: "127.0.0.1:2096"} }

func (o PanelOptions) port() int {
	_, port, _ := net.SplitHostPort(o.Listen)
	n, _ := strconv.Atoi(port)
	return n
}

func (o PanelOptions) origin() string {
	if o.PublicURL != "" {
		return o.PublicURL
	}
	return "http://" + o.Listen
}

// Validate saved settings; ephemeral ports are reserved for development Serve calls.
func (o PanelOptions) Validate() error {
	return o.validate(false)
}

func (o PanelOptions) validate(development bool) error {
	host, port, err := net.SplitHostPort(o.Listen)
	if err != nil {
		return errors.New("监听地址需要 IP:端口，例如 0.0.0.0:2096")
	}
	ip := net.ParseIP(host)
	n, err := strconv.Atoi(port)
	if ip == nil || err != nil || n < 0 || n > 65535 || (n == 0 && (!development || !ip.IsLoopback())) {
		return errors.New("面板监听 IP 或端口无效")
	}
	if (o.Certificate == "") != (o.Key == "") {
		return errors.New("请同时填写 TLS 证书与私钥路径")
	}
	if o.Certificate != "" && len(o.TrustedProxy) > 0 {
		return errors.New("请选择直接 TLS 或 HTTPS 反向代理中的一种方式")
	}
	for _, cidr := range o.TrustedProxy {
		if _, err = netip.ParsePrefix(cidr); err != nil {
			return errors.New("可信代理地址需要 CIDR，例如 127.0.0.1/32")
		}
	}
	if o.Certificate == "" && len(o.TrustedProxy) == 0 && !ip.IsLoopback() {
		return errors.New("public panel requires TLS or an explicitly trusted TLS reverse proxy")
	}
	if o.PublicURL == "" && (!ip.IsLoopback() || len(o.TrustedProxy) > 0 || o.Certificate != "") {
		return errors.New("请填写面板公网 URL，监听 IP 不能用作分享地址")
	}
	if o.PublicURL != "" {
		base, e := url.Parse(o.PublicURL)
		if e != nil || base.Hostname() == "" || base.Path != "" || base.RawPath != "" || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || base.User != nil || !slices.Contains([]string{"http", "https"}, base.Scheme) {
			return errors.New("public URL must be an HTTP(S) origin without a path")
		}
		tlsTransport := o.Certificate != "" || len(o.TrustedProxy) > 0
		if (base.Scheme == "https") != tlsTransport {
			return errors.New("HTTPS URL 需要 TLS 证书或可信 HTTPS 反向代理")
		}
		if !ip.IsLoopback() || tlsTransport {
			if err = shareableURL(o.PublicURL); err != nil {
				return err
			}
		} else if !loopbackHost(base.Hostname()) {
			return errors.New("公网 URL 需要配置 TLS 或可信 HTTPS 反向代理")
		}
	}
	if o.Certificate != "" {
		base, _ := url.Parse(o.PublicURL)
		domain := host
		if base != nil && base.Hostname() != "" {
			domain = base.Hostname()
		}
		material, e := readCertificateMaterial(CertificateAsset{Domain: domain, Certificate: o.Certificate, Key: o.Key})
		if e != nil {
			return e
		}
		if err = material.validateTime(time.Now()); err != nil {
			return err
		}
	}
	return nil
}

func loopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func shareableURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || loopbackHost(u.Hostname()) {
		return errors.New("请先在「公网访问」配置域名或可访问的 IP 与 HTTPS，或启用独立下载监听并填写公网 URL；127.0.0.1/localhost 不能分享给其他人")
	}
	return nil
}

func (s *PlatformService) panelSettings() (PanelOptions, string, error) {
	o := defaultPanelOptions()
	_, err := ReadJSON(filepath.Join(s.Root, "panel.json"), &o)
	data, _ := json.Marshal(o)
	sum := sha256.Sum256(data)
	return o, hex.EncodeToString(sum[:]), err
}

func (s *PlatformService) validatePanelListener(ctx context.Context, options PanelOptions) error {
	n := options.port()
	state, err := s.Store.State(ctx)
	if err != nil {
		return err
	}
	for _, raw := range [][]byte{state.Desired, state.Applied} {
		c, err := decodePlatform(raw)
		if err != nil {
			return err
		}
		if err := panelPortAvailable(c, n); err != nil {
			return err
		}
	}
	return nil
}

func (s *PlatformService) validateConfiguredPanelPort(c PlatformConfig) error {
	panel, _, err := s.panelSettings()
	if err != nil {
		return err
	}
	_, value, err := net.SplitHostPort(panel.Listen)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	return panelPortAvailable(c, port)
}

func samePanelOptions(saved, active PanelOptions) bool {
	saved.PublicURL = saved.origin()
	active.PublicURL = active.origin()
	if len(saved.TrustedProxy) == 0 {
		saved.TrustedProxy = nil
	}
	if len(active.TrustedProxy) == 0 {
		active.TrustedProxy = nil
	}
	return reflect.DeepEqual(saved, active)
}

func panelPortAvailable(c PlatformConfig, port int) error {
	ranges, err := usedPortRanges(c.App, c.VPN)
	if err != nil {
		return err
	}
	if c.Listener.Enabled {
		ranges = append(ranges, portRange{c.Listener.Port, c.Listener.Port})
	}
	for _, service := range c.App.Advanced.Services {
		n := number(service["listen_port"])
		ranges = append(ranges, portRange{n, n})
	}
	if portInRanges(port, ranges) {
		return fmt.Errorf("面板端口 %d 与节点、代理或独立下载监听冲突，请选择其他端口", port)
	}
	return nil
}

func (s *PlatformService) servePanelSettings(w http.ResponseWriter, r *http.Request, access panelAccess, active PanelOptions) bool {
	if r.URL.Path != "/api/panel/settings" && r.URL.Path != "/api/panel/settings/preview" {
		return false
	}
	if !access.require(access.admin) {
		return true
	}
	if r.URL.Path == "/api/panel/settings" && r.Method == "GET" {
		o, revision, err := s.panelSettings()
		if err != nil {
			platformError(w, err)
		} else {
			jsonResponse(w, Object{"options": o, "active": active, "revision": revision, "restart_required": !samePanelOptions(o, active)})
		}
		return true
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	var input struct {
		Options  PanelOptions `json:"options"`
		Revision string       `json:"revision"`
	}
	if err := decodeRequest(w, r, &input, 8192); err != nil {
		platformError(w, err)
		return true
	}
	if err := input.Options.Validate(); err != nil {
		platformError(w, err)
		return true
	}
	if err := s.validatePanelListener(r.Context(), input.Options); err != nil {
		platformError(w, err)
		return true
	}
	if r.URL.Path == "/api/panel/settings/preview" {
		jsonResponse(w, Object{"options": input.Options, "restart_required": !samePanelOptions(input.Options, active)})
		return true
	}
	if !access.audit("panel-settings", "panel.json") {
		return true
	}
	err := withStartupLock(r.Context(), s.Root, func() error {
		_, revision, err := s.panelSettings()
		if err != nil {
			return err
		}
		if revision != input.Revision {
			return controlstore.ErrConflict
		}
		if err := s.validatePanelListener(r.Context(), input.Options); err != nil {
			return err
		}
		return AtomicJSON(filepath.Join(s.Root, "panel.json"), input.Options)
	})
	if err != nil {
		platformError(w, err)
	} else {
		jsonResponse(w, Object{"ok": true, "restart_required": !samePanelOptions(input.Options, active), "public_url": input.Options.PublicURL})
	}
	return true
}
