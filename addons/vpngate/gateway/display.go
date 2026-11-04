package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"unicode"

	"github.com/sck03/sing-box-vpn/internal/manage"
)

func displayText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, value)
}

func writeGatewayStatus(output io.Writer, status Status) error {
	var out bytes.Buffer
	country := status.Country
	if label := manage.Countries[country]; label != "" {
		country = label + " / " + country
	}
	label := "当前国家"
	if status.State == "unavailable" {
		label = "配置国家"
	}
	fmt.Fprintf(&out, " %s：%s\n", label, displayText(country))
	state := map[string]string{
		"ready":       "已连接（出口已验证）",
		"fetching":    "正在获取同国家候选节点",
		"connecting":  "正在建立 OpenVPN 隧道",
		"verifying":   "正在验证出口国家 / 运营商",
		"waiting":     "等待可用节点（转发已阻断）",
		"stopped":     "已暂停 / 停止（转发已阻断）",
		"unavailable": "状态不可用（请检查网关服务）",
	}[status.State]
	if state == "" {
		state = "未知（暂未确认出口）"
	}
	verified := status.State == "ready" && status.Exit != nil && net.ParseIP(status.Exit.IP) != nil && status.Exit.Country == status.Country
	if status.State == "ready" && !verified {
		state = "状态异常（尚无有效的已验证出口）"
	}
	fmt.Fprintf(&out, " 连接状态：%s\n", state)
	exit := "暂无已验证出口"
	if verified {
		exit = status.Exit.IP
	}
	fmt.Fprintf(&out, " 出口 IP：%s\n", displayText(exit))
	mode := "未知"
	switch status.Mode {
	case "sticky":
		mode = "尽量保持节点 / 故障自动重连"
	case "fixed":
		mode = "锁定当前节点"
	}
	fmt.Fprintf(&out, " 连接模式：%s\n", mode)
	if status.Node != "" {
		fmt.Fprintf(&out, " 当前节点：%s\n", displayText(status.Node))
	}
	if verified && status.Exit.ISP != "" {
		fmt.Fprintf(&out, " 出口运营商：%s\n", displayText(status.Exit.ISP))
	}
	if status.Reason != "" && status.State != "ready" && status.State != "stopped" {
		fmt.Fprintf(&out, " 状态说明：%s\n", displayText(status.Reason))
	}
	_, err := output.Write(out.Bytes())
	return err
}
