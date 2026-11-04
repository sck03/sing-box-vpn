package manage

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"unicode"
)

// Configuration values are data, not terminal escape sequences or new lines.
func terminalText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, value)
}

func writeStatusText(output io.Writer, app App, settings VPNSettings, scope, version string) error {
	var out bytes.Buffer
	fmt.Fprintf(&out, " 当前 Sing-box 正式版内核：%s\n", terminalText(version))
	fmt.Fprintf(&out, " 节点入口地址：%s\n", terminalText(app.Address))
	profile, inbounds := app.Profile, app.Inbounds
	if scope == vpngateScope {
		var exists bool
		profile, exists = settings.Profiles[settings.ActiveCountry]
		if !exists {
			return fmt.Errorf("尚未配置 VPNGate 国家")
		}
		inbounds = MakeInbounds(profile, "vg-"+settings.ActiveCountry+"-")
		country := settings.ActiveCountry
		if label := Countries[country]; label != "" {
			country = label + " / " + country
		}
		fmt.Fprintf(&out, " VPNGate 当前国家：%s\n", terminalText(country))
		fmt.Fprintln(&out, " VPNGate 五协议节点：")
	} else {
		fmt.Fprintln(&out, " 普通五协议节点：")
	}
	cache := certificateCache{}
	for _, protocol := range Protocols {
		var inbound Object
		for _, candidate := range inbounds {
			if candidate["type"] == protocol {
				inbound = candidate
				break
			}
		}
		if inbound == nil {
			fmt.Fprintf(&out, " [ %-13s ] 未配置\n", Labels[protocol])
			continue
		}
		tls := object(inbound["tls"])
		mode := "TLS 关闭"
		if tls["enabled"] == true {
			mode = "TLS 已启用"
			if cert, err := cache.get(text(tls["certificate_path"])); err == nil {
				mode = "域名证书"
				if cert.SelfSigned {
					mode = "自签证书"
				}
			}
		}
		if protocol == "vless" {
			mode = "Reality"
		}
		fmt.Fprintf(&out, " [ %-13s ] 端口：%-5d  %s\n", Labels[protocol], number(inbound["listen_port"]), mode)
	}
	fmt.Fprintf(&out, " Reality 域名：%s\n", terminalText(profile.RealitySNI))
	for _, item := range []struct {
		label string
		ports []string
	}{{"Hy2 端口跳跃", profile.Hopping}, {"TUIC 复用端口", profile.TUICPorts}} {
		value := "未设置"
		if len(item.ports) > 0 {
			value = terminalText(strings.Join(item.ports, ", "))
		}
		fmt.Fprintf(&out, " %s：%s\n", item.label, value)
	}
	if scope == normalScope {
		argo := terminalText(text(app.Argo["domain"]))
		if argo == "" {
			argo = "未配置"
		}
		fmt.Fprintf(&out, " Argo 隧道：%s\n", argo)
		fmt.Fprintln(&out, " 域名分流：")
		hasRoutes := false
		for _, route := range []struct{ key, label string }{{"socks", "SOCKS5"}, {"warp", "WARP"}, {"direct4", "IPv4 优先解析"}, {"direct6", "IPv6 优先解析"}} {
			if domains := app.Routing[route.key]; len(domains) > 0 {
				hasRoutes = true
				fmt.Fprintf(&out, "   %s：%s\n", route.label, terminalText(strings.Join(domains, ", ")))
			}
		}
		if !hasRoutes {
			fmt.Fprintln(&out, "   未设置，使用默认直连")
		}
		warp := "未配置（菜单 3 → 6）"
		if len(app.Warp) > 0 {
			warp = "已配置"
		}
		fmt.Fprintf(&out, " WARP endpoint：%s\n", warp)
	} else {
		fmt.Fprintln(&out, " 出口：当前国家的 VPNGate 隧道")
	}
	_, err := output.Write(out.Bytes())
	return err
}
