package manage

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func parseOptions(f *flag.FlagSet, args []string) error {
	var options, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		option := f.Lookup(name)
		if option == nil {
			return fmt.Errorf("未知选项 %s", arg)
		}
		options = append(options, arg)
		boolean, isBoolean := option.Value.(interface{ IsBoolFlag() bool })
		if !strings.Contains(arg, "=") && (!isBoolean || !boolean.IsBoolFlag()) {
			i++
			if i == len(args) {
				return fmt.Errorf("选项 %s 缺少参数", arg)
			}
			options = append(options, args[i])
		}
	}
	return f.Parse(append(options, positional...))
}

func loadVPN(root string) (VPNSettings, map[string]Secrets, error) {
	s := VPNSettings{ProxyPort: 7928, Profiles: map[string]Profile{}}
	path := filepath.Join(root, "vpngate", "settings.json")
	if _, err := ReadJSON(path, &s); err != nil {
		return s, nil, err
	}
	if s.Profiles == nil {
		s.Profiles = map[string]Profile{}
	}
	private := map[string]Secrets{}
	_, err := ReadJSON(filepath.Join(root, "vpngate", "secrets.json"), &private)
	return s, private, err
}

func vpnServer(settings VPNSettings, private map[string]Secrets) (Object, error) {
	country := settings.ActiveCountry
	p, exists := settings.Profiles[country]
	if !exists || !countryPattern.MatchString(country) {
		return nil, errors.New("当前 VPNGate 国家缺少有效入口配置")
	}
	active, exists := private[country]
	if !exists || active.SocksUser == "" || len(active.SocksPass) < 32 {
		return nil, errors.New("国家缺少独立代理凭据")
	}
	p.Secrets = active
	tag := "vg-out-" + country
	return Object{"log": Object{"level": "info", "timestamp": true}, "inbounds": MakeInbounds(p, "vg-"+country+"-"),
		"outbounds": []Object{{"type": "socks", "tag": tag, "server": "127.0.0.1", "server_port": settings.ProxyPort, "version": "5", "username": active.SocksUser, "password": active.SocksPass}},
		"route":     Object{"final": tag}}, nil
}

func hexSecret() string { return fmt.Sprintf("%x", randomBytes(32)) }
func usedPorts(app App, settings VPNSettings) map[int]bool {
	used := map[int]bool{settings.ProxyPort: true, app.SocksPort: true}
	for _, inbound := range app.Inbounds {
		used[number(inbound["listen_port"])] = true
	}
	for _, p := range settings.Profiles {
		for _, port := range p.Ports {
			used[port] = true
		}
	}
	return used
}

func publicIP(ctx context.Context) (string, error) {
	client := newHTTPClient(10 * time.Second)
	defer client.CloseIdleConnections()
	for _, url := range []string{"https://api4.ipify.org", "https://ipv4.icanhazip.com"} {
		response, err := httpRequest(ctx, client, url)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 128))
		response.Body.Close()
		ip := net.ParseIP(strings.TrimSpace(string(data)))
		if err == nil && ip != nil && ip.To4() != nil {
			return ip.String(), nil
		}
	}
	return "", errors.New("无法取得 VPS IPv4，请使用 --address 指定")
}

var argoPattern = regexp.MustCompile(`https://([a-z0-9-]+\.trycloudflare\.com)`)
