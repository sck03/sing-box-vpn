package manage

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Legacy command syntax is used exclusively by pre-platform test fixtures.
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
	if platformOwned(*root) && !slices.Contains([]string{"show", "recover", "links", "export", "subscription-urls"}, operation) {
		return errors.New("configuration is owned by control.db; use the unified panel or panel apply")
	}
	options := flag.NewFlagSet(operation, flag.ContinueOnError)
	options.SetOutput(output)
	address, name, port := options.String("address", "", "server address"), options.String("name", "VPS", "node name"), options.Int("port", 0, "subscription port")
	timeout := options.Duration("timeout", 90*time.Second, "Argo wait timeout")
	scope := options.String("scope", normalScope, "normal or vpngate nodes")
	format := options.String("format", "json", "show output: json or text")
	stdin := options.Bool("stdin", false, "read custom UUID/password from stdin")
	tuicPort := options.Int("tuic-port", 0, "TUIC port for a single share link")
	listen := options.String("listen", "", "subscription listen IP")
	publicBase := options.String("public-base-url", "", "subscription public base URL")
	applyServices := options.Bool("apply-services", false, "apply subscription settings with service rollback")
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
	if operation == "recover" {
		return recoverConfig(ctx, *root)
	}
	execute := func() error {
		var app App
		exists, err := ReadJSON(filepath.Join(*root, "app.json"), &app)
		if err != nil {
			return err
		}
		if operation == "init" {
			if exists {
				return CommitApp(ctx, *root, *core, &app)
			}
			return initializeApp(ctx, *root, *core, *address, *name)
		}
		if !exists {
			return errors.New("请先安装普通节点")
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
			return configureSubscription(*root, app, settings, *port, *listen, *publicBase)
		case "subscription-reset":
			return resetSubscriptionToken(*root)
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
			if operation == "links" {
				dir, _ := exportDirectory(*root, *scope)
				data, err := readSubscriptionFile(filepath.Join(dir, "jhsub.txt"))
				if err != nil {
					return err
				}
				_, err = output.Write(data)
				return err
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
			if operation == "multiport" && values[0] == "tuic" {
				return commitTUICPorts(*root, app, settings, *scope, values[1:])
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
	}
	if *applyServices {
		if operation != "subscription-init" && operation != "subscription-stop" {
			return errors.New("--apply-services 仅用于订阅启停")
		}
		return withOperationLock(*root, func() error {
			return applySubscriptionChange(ctx, *root, execute, systemSubscriptionService{root: *root})
		})
	}
	return withLock(*root, execute)
}

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
