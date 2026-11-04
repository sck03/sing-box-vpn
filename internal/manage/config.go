package manage

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type fileBundle map[string][]byte

const normalScope = "normal"
const vpngateScope = "vpngate"

var exportNames = []string{"sbox.json", "clmi.yaml", "jhsub.txt", "jhsub-base64.txt", "share.json"}

func exportDirectory(root, scope string) (string, error) {
	switch scope {
	case normalScope:
		return root, nil
	case vpngateScope:
		return filepath.Join(root, "vpngate"), nil
	default:
		return "", errors.New("节点类型必须为 normal 或 vpngate")
	}
}

func (files fileBundle) json(name string, value any) error {
	data, err := JSONBytes(value)
	if err == nil {
		files[name] = data
	}
	return err
}

// Callers validate the whole bundle first; only changed files need staging and fsync.
func publish(root string, files fileBundle) error {
	names := make([]string, 0, len(files))
	for name := range files {
		if !filepath.IsLocal(name) {
			return errors.New("无效配置路径")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	type previousFile struct {
		data   []byte
		exists bool
	}
	previous := map[string]previousFile{}
	changed := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err != nil && files[name] == nil {
			continue
		}
		if err == nil && files[name] != nil && bytes.Equal(data, files[name]) {
			info, statErr := os.Lstat(path)
			if statErr != nil {
				return statErr
			}
			if info.Mode().IsRegular() {
				if info.Mode().Perm() != 0600 {
					if err := os.Chmod(path, 0600); err != nil {
						return err
					}
				}
				continue
			}
		}
		previous[name] = previousFile{data, err == nil}
		changed = append(changed, name)
	}
	if len(changed) == 0 {
		return nil
	}
	stage, err := os.MkdirTemp(root, ".commit-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, name := range changed {
		if data := files[name]; data != nil {
			if err := AtomicBytes(filepath.Join(stage, name), data, 0600); err != nil {
				return err
			}
		}
	}
	var applied []string
	for _, name := range changed {
		path := filepath.Join(root, name)
		if files[name] == nil {
			err = os.Remove(path)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		} else {
			if err = os.MkdirAll(filepath.Dir(path), 0700); err == nil {
				err = os.Rename(filepath.Join(stage, name), path)
			}
		}
		if err != nil {
			for _, changed := range applied {
				before := previous[changed]
				target := filepath.Join(root, changed)
				if before.exists {
					err = errors.Join(err, AtomicBytes(target, before.data, 0600))
				} else {
					removeErr := os.Remove(target)
					if !errors.Is(removeErr, os.ErrNotExist) {
						err = errors.Join(err, removeErr)
					}
				}
			}
			return err
		}
		applied = append(applied, name)
	}
	return nil
}

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
func exportBundle(ctx context.Context, root, core string, app App, settings VPNSettings, private map[string]Secrets) (fileBundle, map[string][]string, error) {
	normal, vpn, mNormal, mVPN := []Object{}, []Object{}, []Object{}, []Object{}
	links := map[string][]string{normalScope: {}, vpngateScope: {}}
	cache := certificateCache{}
	add := func(inbound Object, profile Profile, name string, gateway bool) error {
		p, err := outboundProfile(inbound, profile, cache)
		if err != nil {
			return err
		}
		n, err := outboundWithCache(inbound, p, app.Address, name, cache)
		if err != nil {
			return err
		}
		if !gateway && text(n["type"]) == "vmess" && app.CDNAddress != "" {
			n["server"] = app.CDNAddress
		}
		link, err := uriWithCache(n, p, cache)
		if err != nil {
			return err
		}
		if gateway {
			links[vpngateScope] = append(links[vpngateScope], link)
			vpn = append(vpn, n)
			mVPN = append(mVPN, MihomoNode(n, p))
		} else {
			links[normalScope] = append(links[normalScope], link)
			normal = append(normal, n)
			mNormal = append(mNormal, MihomoNode(n, p))
		}
		return nil
	}
	for _, inbound := range app.Inbounds {
		kind := text(inbound["type"])
		if Labels[kind] == "" {
			return nil, nil, errors.New("未知入口协议")
		}
		if err := add(inbound, app.Profile, Labels[kind]+"-普通-"+app.Name, false); err != nil {
			return nil, nil, err
		}
	}
	if domain := text(app.Argo["domain"]); domain != "" {
		var vmess Object
		for _, node := range normal {
			if node["type"] == "vmess" {
				vmess = clone(node).(Object)
				break
			}
		}
		if vmess == nil {
			return nil, nil, errors.New("Argo 缺少 VMess 入口")
		}
		address := text(app.Argo["cdn_address"])
		if address == "" {
			address = domain
		}
		vmess["tag"], vmess["server"], vmess["server_port"] = "VMess-Argo-普通-"+app.Name, address, 443
		vmess["tls"] = Object{"enabled": true, "server_name": domain}
		ensureObject(vmess, "transport")["headers"] = Object{"Host": domain}
		p := app.Profile
		p.TLSSNI, p.SelfSigned = domain, false
		link, err := uriWithCache(vmess, p, cache)
		if err != nil {
			return nil, nil, err
		}
		links[normalScope] = append(links[normalScope], link)
		normal = append(normal, vmess)
		mNormal = append(mNormal, MihomoNode(vmess, p))
	}
	if cc := settings.ActiveCountry; cc != "" {
		p, exists := settings.Profiles[cc]
		if !exists {
			return nil, nil, errors.New("当前 VPNGate 国家缺少入口配置")
		}
		secret, exists := private[cc]
		if !exists {
			return nil, nil, fmt.Errorf("国家 %s 缺少独立凭据", cc)
		}
		p.Secrets = secret
		label := Countries[cc]
		if label == "" {
			label = cc
		}
		for _, inbound := range MakeInbounds(p, "vg-"+cc+"-") {
			if err := add(inbound, p, "VPNGate-"+cc+"-"+label+"-"+Labels[text(inbound["type"])]+"-家宽候选", true); err != nil {
				return nil, nil, err
			}
		}
	}
	files := fileBundle{}
	for _, set := range []struct {
		scope         string
		nodes, mihomo []Object
	}{{normalScope, normal, mNormal}, {vpngateScope, vpn, mVPN}} {
		gateway := set.scope == vpngateScope
		prefix := ""
		if gateway {
			prefix = "vpngate/"
			if len(set.nodes) == 0 {
				for _, name := range exportNames {
					files[prefix+name] = nil
				}
				continue
			}
		}
		client := ClientConfig(set.nodes, gateway)
		if err := CheckJSON(ctx, core, root, client); err != nil {
			return nil, nil, err
		}
		if err := files.json(prefix+"sbox.json", client); err != nil {
			return nil, nil, err
		}
		if err := files.json(prefix+"clmi.yaml", MihomoConfig(set.mihomo, gateway)); err != nil {
			return nil, nil, err
		}
		joined := strings.Join(links[set.scope], "\n")
		files[prefix+"jhsub.txt"] = []byte(joined + "\n")
		files[prefix+"jhsub-base64.txt"] = []byte(base64.StdEncoding.EncodeToString([]byte(joined)) + "\n")
		share := Object{"links": links[set.scope], "normal": []string{}, "vpngate": []string{}}
		names := make([]string, 0, len(set.nodes))
		for _, n := range set.nodes {
			names = append(names, text(n["tag"]))
		}
		share[set.scope] = names
		if err := files.json(prefix+"share.json", share); err != nil {
			return nil, nil, err
		}
	}
	return files, links, nil
}

func CommitApp(ctx context.Context, root, core string, app *App) error {
	version, err := CheckCore(ctx, core)
	if err != nil {
		return err
	}
	app.Version = version
	syncProfileSecrets(&app.Profile, app.Inbounds)
	server, err := ServerConfig(app.Inbounds, app.Routing, app.Warp, app.SocksPort)
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
	if !exists || p.Ports == nil || secret.UUID == nil || secret.Passwords == nil {
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
	current := App{Profile: p, Inbounds: MakeInbounds(p, "vg-"+cc+"-")}
	if err := mutateApp(&current, settings, operation, values); err != nil {
		return err
	}
	p = current.Profile
	secret = p.Secrets
	for _, in := range current.Inbounds {
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

func ImportExisting(root string) (Profile, []Object, map[string][]string, Object, error) {
	existing := Object{}
	found, err := ReadJSON(filepath.Join(root, "sb.json"), &existing)
	if err != nil {
		return Profile{}, nil, nil, nil, err
	}
	used := map[int]bool{40000: true, 7928: true}
	for _, inbound := range objects(existing["inbounds"]) {
		used[number(inbound["listen_port"])] = true
	}
	if found {
		if err = AtomicJSON(filepath.Join(root, "migration-backup.json"), existing); err != nil {
			return Profile{}, nil, nil, nil, err
		}
	}
	p, err := NewProfile(root, used, "")
	if err != nil {
		return p, nil, nil, nil, err
	}
	if !found {
		return p, MakeInbounds(p, ""), map[string][]string{}, nil, nil
	}
	original, fresh := map[string]Object{}, map[string]Object{}
	for _, in := range objects(existing["inbounds"]) {
		original[text(in["type"])] = in
	}
	for _, in := range MakeInbounds(p, "") {
		fresh[text(in["type"])] = in
	}
	keep := []string{"type", "tag", "listen", "listen_port", "users", "tls", "transport", "ignore_client_bandwidth", "congestion_control", "padding_scheme"}
	result := []Object{}
	for _, protocol := range Protocols {
		old := original[protocol]
		if old == nil {
			result = append(result, fresh[protocol])
			continue
		}
		in := Object{}
		for _, key := range keep {
			if value, exists := old[key]; exists {
				in[key] = clone(value)
			}
		}
		in["tag"] = Tags[protocol]
		p.Ports[protocol] = number(in["listen_port"])
		if protocol == "vless" {
			tls := object(in["tls"])
			reality := object(tls["reality"])
			private := text(reality["private_key"])
			public, err := derivePublic(private)
			if err != nil {
				return p, nil, nil, nil, err
			}
			ids := stringsValue(reality["short_id"])
			shortID := ""
			if len(ids) > 0 {
				shortID = ids[0]
			}
			p.Secrets.Reality = Reality{private, public, shortID}
			p.RealitySNI = text(tls["server_name"])
		}
		if protocol == "vmess" {
			p.WSPath = text(object(in["transport"])["path"])
			p.VMessTLS = object(in["tls"])["enabled"] == true
		}
		if protocol != "vless" && object(in["tls"])["enabled"] == true {
			tls := object(in["tls"])
			if _, err = os.Stat(text(tls["certificate_path"])); err != nil {
				return p, nil, nil, nil, fmt.Errorf("旧节点证书缺失，已保留迁移备份: %w", err)
			}
			if _, err = os.Stat(text(tls["key_path"])); err != nil {
				return p, nil, nil, nil, err
			}
		}
		result = append(result, in)
	}
	routing := map[string][]string{}
	for _, rule := range objects(object(existing["route"])["rules"]) {
		target := text(rule["outbound"])
		name := ""
		switch {
		case strings.Contains(target, "warp") || strings.Contains(target, "wireguard"):
			name = "warp"
		case strings.Contains(target, "socks"):
			name = "socks"
		case target == "vps-outbound-v4":
			name = "direct4"
		case target == "vps-outbound-v6":
			name = "direct6"
		}
		if name != "" {
			for _, domain := range stringsValue(rule["domain_suffix"]) {
				if domain != "yg_kkk" {
					routing[name] = append(routing[name], domain)
				}
			}
		}
	}
	var warp Object
	for _, endpoint := range objects(existing["endpoints"]) {
		if endpoint["type"] == "wireguard" {
			warp = clone(endpoint).(Object)
			break
		}
	}
	if warp == nil {
		for _, old := range objects(existing["outbounds"]) {
			if old["type"] == "wireguard" {
				reserved := old["reserved"]
				if reserved == nil {
					reserved = []int{0, 0, 0}
				}
				warp = Object{"type": "wireguard", "tag": "warp-out", "address": old["local_address"], "private_key": old["private_key"], "peers": []Object{{"address": old["server"], "port": old["server_port"], "public_key": old["peer_public_key"], "allowed_ips": []string{"0.0.0.0/0", "::/0"}, "reserved": reserved}}}
				break
			}
		}
	}
	syncProfileSecrets(&p, result)
	return p, result, routing, warp, nil
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

func RunConfig(ctx context.Context, args []string, output io.Writer) error {
	f := flag.NewFlagSet("config", flag.ContinueOnError)
	f.SetOutput(output)
	root := f.String("root", "/etc/s-box", "configuration directory")
	core := f.String("core", "", "sing-box binary")
	if err := f.Parse(args); err != nil {
		return err
	}
	args = f.Args()
	if len(args) == 0 {
		return errors.New("缺少配置操作")
	}
	if *core == "" {
		*core = filepath.Join(*root, "sing-box")
	}
	operation := args[0]
	options := flag.NewFlagSet(operation, flag.ContinueOnError)
	options.SetOutput(output)
	address, name, port := options.String("address", "", "server address"), options.String("name", "VPS", "node name"), options.Int("port", 0, "subscription port")
	timeout := options.Duration("timeout", 90*time.Second, "Argo wait timeout")
	scope := options.String("scope", normalScope, "normal or vpngate nodes")
	format := options.String("format", "json", "show output: json or text")
	stdin := options.Bool("stdin", false, "read custom UUID/password from stdin")
	tuicPort := options.Int("tuic-port", 0, "TUIC port for a single share link")
	if err := parseOptions(options, args[1:]); err != nil {
		return err
	}
	tuicRequested := false
	options.Visit(func(option *flag.Flag) {
		if option.Name == "tuic-port" {
			tuicRequested = true
		}
	})
	if *format != "json" && (*format != "text" || operation != "show") {
		return errors.New("show 支持 --format json 或 text")
	}
	if _, err := exportDirectory(*root, *scope); err != nil {
		return err
	}
	if *scope == vpngateScope && !slices.Contains([]string{"show", "export", "links", "subscription-urls", "port", "hopping", "multiport", "credentials", "reset", "sni", "ws", "tls"}, operation) {
		return errors.New("此操作不支持 --scope vpngate")
	}
	values := options.Args()
	if *stdin {
		if operation != "credentials" || len(values) != 1 {
			return errors.New("--stdin 仅用于 credentials，并需要指定一个协议或 all")
		}
		credentials, err := readCredentials(os.Stdin)
		if err != nil {
			return err
		}
		values = append(values, credentials...)
	}
	if tuicRequested && (operation != "links" || *tuicPort < 1 || *tuicPort > 65535) {
		return errors.New("--tuic-port 仅用于 links，端口须为 1-65535")
	}
	if operation == "argo-watch" {
		return waitArgo(ctx, *root, *core, *timeout, output)
	}
	return withLock(*root, func() error {
		var app App
		exists, err := ReadJSON(filepath.Join(*root, "app.json"), &app)
		if err != nil {
			return err
		}
		if operation == "init" {
			if _, err = CheckCore(ctx, *core); err != nil {
				return err
			}
			if exists {
				return CommitApp(ctx, *root, *core, &app)
			}
			profile, inbounds, routing, warp, err := ImportExisting(*root)
			if err != nil {
				return err
			}
			if *address == "" {
				if data, err := os.ReadFile(filepath.Join(*root, "server_ipcl.log")); err == nil {
					*address = strings.TrimSpace(string(data))
				}
			}
			if *address == "" {
				*address, err = publicIP(ctx)
				if err != nil {
					return err
				}
			}
			app = App{Name: *name, Address: *address, Profile: profile, Inbounds: inbounds, Routing: routing, Warp: warp, SocksPort: 40000}
			return CommitApp(ctx, *root, *core, &app)
		}
		if !exists {
			return errors.New("请先安装或迁移普通节点")
		}
		settings, private, err := loadVPN(*root)
		if err != nil {
			return err
		}
		switch operation {
		case "show":
			version, err := CheckCore(ctx, *core)
			if *format == "text" {
				if err != nil {
					version = "不可用（菜单 8 检查核心）"
				}
				return writeStatusText(output, app, settings, *scope, version)
			}
			if err != nil {
				return err
			}
			ports := Object{}
			if *scope == vpngateScope {
				p, exists := settings.Profiles[settings.ActiveCountry]
				if !exists {
					return errors.New("尚未配置 VPNGate 国家")
				}
				for kind, port := range p.Ports {
					ports[kind] = port
				}
			} else {
				for _, in := range app.Inbounds {
					ports[text(in["type"])] = in["listen_port"]
				}
			}
			profile := app.Profile
			if *scope == vpngateScope {
				profile = settings.Profiles[settings.ActiveCountry]
			}
			status := Object{"core": version, "address": app.Address, "protocols": ports, "scope": *scope,
				"multiport": Object{"hysteria2": profile.Hopping, "tuic": profile.TUICPorts}}
			if *scope == vpngateScope {
				status["country"] = settings.ActiveCountry
			} else {
				status["routing"] = app.Routing
			}
			data, err := JSONBytes(status)
			if err == nil {
				_, err = output.Write(data)
			}
			return err
		case "vmess-info":
			for _, in := range app.Inbounds {
				if in["type"] == "vmess" {
					scheme := "http"
					if object(in["tls"])["enabled"] == true {
						scheme = "https"
					}
					_, err := fmt.Fprintf(output, "%d %s\n", number(in["listen_port"]), scheme)
					return err
				}
			}
			return errors.New("缺少 VMess 入口")
		case "subscription-init":
			return configureSubscription(*root, app, settings, *port)
		case "subscription-stop":
			return disableSubscription(*root)
		case "subscription-urls":
			return subscriptionURLs(*root, app.Address, *scope, output)
		case "export", "links":
			if *scope == vpngateScope && settings.ActiveCountry == "" {
				return errors.New("请先通过菜单 17 安装并选择 VPNGate 国家")
			}
			if tuicRequested {
				return writeTUICLink(output, app, settings, private, *scope, *tuicPort)
			}
			files, links, err := exportBundle(ctx, *root, *core, app, settings, private)
			if err != nil {
				return err
			}
			if err = publish(*root, files); err != nil {
				return err
			}
			if operation == "links" {
				for _, link := range links[*scope] {
					fmt.Fprintln(output, link)
				}
			}
			return nil
		case "vpngate":
			if len(values) != 1 {
				return errors.New("需要国家代码")
			}
			_, err := PrepareVPN(ctx, *root, *core, app, values[0])
			return err
		case "vpngate-remove":
			files, _, err := exportBundle(ctx, *root, *core, app, VPNSettings{Profiles: map[string]Profile{}}, nil)
			if err != nil {
				return err
			}
			files["vpngate/settings.json"] = nil
			return publish(*root, files)
		}
		if slices.Contains([]string{"port", "hopping", "multiport"}, operation) {
			if err := validatePortMutation(*root, app, settings, *scope, operation, values); err != nil {
				return err
			}
		}
		if *scope == vpngateScope {
			if err := mutateVPN(app, settings, private, operation, values); err != nil {
				return err
			}
			return commitVPN(ctx, *root, *core, app, settings, private, false)
		}
		if err := mutateApp(&app, settings, operation, values); err != nil {
			return err
		}
		return CommitApp(ctx, *root, *core, &app)
	})
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
	case "reset", "credentials":
		return changeCredentials(app, operation, values)
	case "route":
		if len(values) == 0 || !slices.Contains([]string{"socks", "warp", "direct4", "direct6"}, values[0]) {
			return errors.New("未知分流通道")
		}
		domains := []string{}
		for _, domain := range values[1:] {
			domain = strings.TrimLeft(domain, ".")
			if err := validateDomain(domain); err != nil {
				return err
			}
			domains = append(domains, domain)
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
		for _, in := range app.Inbounds {
			if in["type"] == "vless" {
				tls := ensureObject(in, "tls")
				tls["server_name"] = values[0]
				ensureObject(ensureObject(tls, "reality"), "handshake")["server"] = values[0]
			}
		}
		app.Profile.RealitySNI = values[0]
	case "ws":
		if err := count(1); err != nil {
			return err
		}
		if len(values[0]) == 0 || len(values[0]) > 255 || strings.ContainsAny(values[0], "\r\n") {
			return errors.New("路径格式错误")
		}
		for _, in := range app.Inbounds {
			if in["type"] == "vmess" {
				ensureObject(in, "transport")["path"] = values[0]
			}
		}
		app.Profile.WSPath = values[0]
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
		if err = EnsureCertificate(cert, key, values[3]); err != nil {
			return err
		}
		for _, in := range app.Inbounds {
			if in["type"] == "vless" || values[0] != "all" && values[0] != in["type"] {
				continue
			}
			in["tls"] = merge(object(in["tls"]), Object{"enabled": true, "certificate_path": filepath.ToSlash(cert), "key_path": filepath.ToSlash(key), "server_name": values[3]})
		}
		app.Profile.TLSSNI, app.Profile.SelfSigned = values[3], false
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
		app.Warp = warp
	default:
		return errors.New("未知配置操作")
	}
	return nil
}

var argoPattern = regexp.MustCompile(`https://([a-z0-9-]+\.trycloudflare\.com)`)

func waitArgo(ctx context.Context, root, core string, timeout time.Duration, output io.Writer) error {
	child, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		f, err := os.Open(filepath.Join(root, "argo.log"))
		if err == nil {
			info, statErr := f.Stat()
			if statErr == nil && info.Size() > 64<<10 {
				_, _ = f.Seek(-(64 << 10), io.SeekEnd)
			}
			data, _ := io.ReadAll(io.LimitReader(f, 64<<10))
			f.Close()
			matches := argoPattern.FindAllStringSubmatch(string(data), -1)
			if len(matches) > 0 {
				return RunConfig(child, []string{"--root", root, "--core", core, "argo", matches[len(matches)-1][1]}, output)
			}
		}
		select {
		case <-child.Done():
			return errors.New("临时 Argo 域名未生成，请查看 sb-argo 日志")
		case <-tick.C:
		}
	}
}
