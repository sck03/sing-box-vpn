package manage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"

	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/publicaccess"
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
	return publicaccess.Options(o).Validate(publicaccess.Rules{Development: development})
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
		if pending, err := s.Store.PendingKind(r.Context(), "panel-restart"); err != nil {
			return err
		} else if pending {
			return fmt.Errorf("面板访问重启任务尚未结束，请稍后保存")
		}
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
