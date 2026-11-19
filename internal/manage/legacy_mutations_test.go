package manage

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Historical mutation helpers are isolated test fixtures, never production configuration writers.
func CommitApp(ctx context.Context, root, core string, app *App) error {
	managed, err := managedNodes(*app)
	if err != nil {
		return err
	}
	if len(managed) != len(Protocols) {
		return errors.New("普通配置必须完整包含五协议")
	}
	version, err := CheckCore(ctx, core)
	if err != nil {
		return err
	}
	app.Version = version
	server, err := serverForApp(*app)
	if err != nil {
		return err
	}
	if err = CheckJSON(ctx, core, root, server); err != nil {
		return err
	}
	settings, private, err := loadVPN(root)
	if err != nil {
		return err
	}
	files, _, err := exportBundle(ctx, root, core, *app, settings, private)
	if err != nil {
		return err
	}
	if err = files.json("sb.json", server); err != nil {
		return err
	}
	if err = files.json("app.json", app); err != nil {
		return err
	}
	if settings.ActiveCountry != "" {
		gateway, err := vpnServer(settings, private)
		if err != nil {
			return err
		}
		if err := CheckJSON(ctx, core, root, gateway); err != nil {
			return err
		}
		if err := files.json("vpngate/server.json", gateway); err != nil {
			return err
		}
	}
	return publish(root, files)
}

func PrepareVPN(ctx context.Context, root, core string, app App, country string) (VPNSettings, error) {
	country = strings.ToUpper(country)
	if !countryPattern.MatchString(country) {
		return VPNSettings{}, errors.New("请输入两位国家代码，例如 JP、KR、US")
	}
	s, private, err := loadVPN(root)
	if err != nil {
		return s, err
	}
	if _, exists := s.Profiles[country]; !exists {
		used, ranges, err := allocationPorts(root, app, s)
		if err != nil {
			return s, err
		}
		p, err := NewProfile(root, used, country+"-", ranges...)
		if err != nil {
			return s, err
		}
		secret := p.Secrets
		secret.SocksUser, secret.SocksPass = "ygvg-"+country, hexSecret()
		private[country] = secret
		p.Secrets, p.Country = Secrets{}, country
		s.Profiles[country] = p
		s.Order = append(s.Order, country)
	}
	s.ActiveCountry = country
	return s, commitVPN(ctx, root, core, app, s, private, true)
}

func commitVPN(ctx context.Context, root, core string, app App, s VPNSettings, private map[string]Secrets, activate bool) error {
	country := s.ActiveCountry
	server, err := vpnServer(s, private)
	if err != nil {
		return err
	}
	active := private[country]
	gateway := Object{}
	if _, err := ReadJSON(filepath.Join(root, "vpngate", "gateway.json"), &gateway); err != nil {
		return err
	}
	if gateway["country"] != country {
		gateway["mode"], gateway["fixed_host"] = "sticky", ""
	}
	dataDir, runDir := os.Getenv("YGVG_DATA_DIR"), os.Getenv("YGVG_RUN_DIR")
	if dataDir == "" {
		dataDir = text(gateway["data_dir"])
	}
	if runDir == "" {
		runDir = text(gateway["run_dir"])
	}
	if dataDir == "" {
		dataDir = "/var/lib/yg-vpngate"
	}
	if runDir == "" {
		runDir = "/run/yg-vpngate"
	}
	gateway = merge(gateway, Object{"country": country, "listen": "127.0.0.1:" + strconv.Itoa(s.ProxyPort), "proxy_user": active.SocksUser, "proxy_pass": active.SocksPass, "data_dir": dataDir, "run_dir": runDir})
	if activate {
		gateway["enabled"] = true
	}
	if err := CheckJSON(ctx, core, filepath.Join(root, "vpngate"), server); err != nil {
		return err
	}
	files, _, err := exportBundle(ctx, root, core, app, s, private)
	if err != nil {
		return err
	}
	for name, value := range map[string]any{"vpngate/secrets.json": private, "vpngate/settings.json": s, "vpngate/gateway.json": gateway, "vpngate/server.json": server} {
		if err = files.json(name, value); err != nil {
			return err
		}
	}
	return publish(root, files)
}

func mutateVPN(app App, settings VPNSettings, private map[string]Secrets, operation string, values []string) error {
	cc := settings.ActiveCountry
	p, exists := settings.Profiles[cc]
	if !exists || cc == "" {
		return errors.New("请先通过菜单 17 安装并选择 VPNGate 国家")
	}
	secret, exists := private[cc]
	if !exists || p.Ports == nil {
		return errors.New("当前 VPNGate 国家缺少独立凭据")
	}
	if !slices.Contains([]string{"port", "hopping", "multiport", "credentials", "reset", "sni", "ws", "tls"}, operation) {
		return errors.New("VPNGate 支持 port、hopping、multiport、credentials、reset、sni、ws 和 tls 设置")
	}
	if operation == "tls" && (len(values) == 0 || values[0] != "all") {
		return errors.New("VPNGate 的 TLS 证书统一设置，请使用 tls all；VLESS 保持 Reality")
	}
	if slices.Contains([]string{"port", "hopping", "multiport"}, operation) {
		if err := validatePortMutation("", app, settings, vpngateScope, operation, values); err != nil {
			return err
		}
	}
	p.Secrets, p.Country = secret, cc
	current := App{Nodes: MakeNodes(p, "vg-"+cc+"-"), Country: p.Country, Hopping: p.Hopping, TUICPorts: p.TUICPorts}
	var err error
	if slices.Contains([]string{"port", "hopping", "multiport"}, operation) {
		protocol, ranges, _ := portMutation(operation, values)
		err = applyPorts(&current, operation, protocol, ranges)
	} else {
		err = mutateApp(&current, settings, operation, values)
	}
	if err != nil {
		return err
	}
	p = current.fixtureProfile()
	p.Secrets.SocksUser, p.Secrets.SocksPass = secret.SocksUser, secret.SocksPass
	secret = p.Secrets
	for _, in := range current.Inbounds() {
		kind := text(in["type"])
		if operation == "tls" && kind == "vmess" {
			tls := object(in["tls"])
			p.Certificate, p.Key, p.VMessTLS = text(tls["certificate_path"]), text(tls["key_path"]), true
		}
	}
	p.Secrets = Secrets{}
	settings.Profiles[cc], private[cc] = p, secret
	return nil
}

func mutateApp(app *App, settings VPNSettings, operation string, values []string) error {
	count := func(n int) error {
		if len(values) != n {
			return fmt.Errorf("%s 需要 %d 个参数", operation, n)
		}
		return nil
	}
	protocol := func(value string) bool { return slices.Contains(Protocols, value) }
	switch operation {
	case "port", "hopping", "multiport":
		return changePorts(app, settings, operation, values)
	case "route":
		if len(values) == 0 || !slices.Contains([]string{"socks", "warp", "direct4", "direct6"}, values[0]) {
			return errors.New("未知分流通道")
		}
		domains := []string{}
		seen := make(map[string]bool, len(values)-1)
		for _, domain := range values[1:] {
			domain = strings.ToLower(strings.TrimPrefix(domain, "."))
			if err := validateDomain(domain); err != nil {
				return err
			}
			if !seen[domain] {
				domains = append(domains, domain)
				seen[domain] = true
			}
		}
		if app.Routing == nil {
			app.Routing = map[string][]string{}
		}
		app.Routing[values[0]] = domains
	case "socks":
		if err := count(1); err != nil {
			return err
		}
		p, err := strconv.Atoi(values[0])
		if err != nil || p < 1 || p > 65535 {
			return errors.New("端口错误")
		}
		app.SocksPort = p
	case "address", "cdn":
		if operation == "cdn" && (len(values) == 0 || len(values) == 1 && values[0] == "") {
			app.CDNAddress = ""
			return nil
		}
		if err := count(1); err != nil {
			return err
		}
		if net.ParseIP(values[0]) == nil {
			if err := validateDomain(values[0]); err != nil {
				return err
			}
		}
		if operation == "address" {
			app.Address = values[0]
		} else {
			app.CDNAddress = values[0]
		}
	case "sni":
		if err := count(1); err != nil {
			return err
		}
		if err := validateDomain(values[0]); err != nil {
			return err
		}
		for i := range app.Nodes {
			if app.Nodes[i].Protocol == "vless" {
				app.Nodes[i].TLS.ServerName = values[0]
				app.Nodes[i].TLS.HandshakeHost = values[0]
			}
		}
	case "ws":
		if err := count(1); err != nil {
			return err
		}
		if err := validateWebSocketPath(values[0]); err != nil {
			return err
		}
		for i := range app.Nodes {
			if app.Nodes[i].Protocol == "vmess" {
				app.Nodes[i].Transport.Path = values[0]
			}
		}
	case "tls":
		if err := count(4); err != nil {
			return err
		}
		if values[0] != "all" && !protocol(values[0]) {
			return errors.New("未知协议")
		}
		if values[0] == "vless" {
			return errors.New("VLESS 使用 Reality，请通过 SNI 菜单设置")
		}
		if err := validateDomain(values[3]); err != nil {
			return err
		}
		cert, err := filepath.Abs(values[1])
		if err != nil {
			return err
		}
		key, err := filepath.Abs(values[2])
		if err != nil {
			return err
		}
		if err = validateImportedCertificate(cert, key, values[3]); err != nil {
			return err
		}
		for i := range app.Nodes {
			n := &app.Nodes[i]
			if n.Protocol == "vless" || values[0] != "all" && values[0] != n.Protocol {
				continue
			}
			n.TLS = ManagedTLS{Enabled: true, Certificate: filepath.ToSlash(cert), Key: filepath.ToSlash(key), ServerName: values[3]}
		}
	case "argo":
		if len(values) > 1 {
			return errors.New("域名参数过多")
		}
		domain := ""
		if len(values) == 1 {
			domain = values[0]
			if domain != "" {
				if err := validateDomain(domain); err != nil {
					return err
				}
			}
		}
		app.Argo = Object{"domain": domain}
	case "warp":
		if err := count(1); err != nil {
			return err
		}
		var warp Object
		found, err := ReadJSON(values[0], &warp)
		if err != nil {
			return err
		}
		if !found || warp["type"] != "wireguard" || len(objects(warp["peers"])) == 0 || text(warp["private_key"]) == "" {
			return errors.New("请输入新版 WireGuard endpoint JSON")
		}
		app.Advanced.Endpoints = []Object{merge(warp, Object{"tag": "warp-out"})}
	default:
		return errors.New("未知配置操作")
	}
	return nil
}
