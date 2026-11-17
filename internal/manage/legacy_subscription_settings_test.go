package manage

import (
	"errors"
	"fmt"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

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
