package vpngate

import (
	"bytes"
	"fmt"
	"io"
	"net"

	"github.com/sck03/sing-box-vpn/internal/country"
	"github.com/sck03/sing-box-vpn/internal/terminal"
)

func writeGatewayStatus(output io.Writer, status Status) error {
	var out bytes.Buffer
	countryText := status.Country
	if label := country.Names[status.Country]; label != "" {
		countryText = label + " / " + countryText
	}
	label := "当前国家"
	if status.State == Unavailable {
		label = "配置国家"
	}
	fmt.Fprintf(&out, " %s：%s\n", label, terminal.Text(countryText))
	state := map[State]string{
		Ready:       "已连接（出口已验证）",
		Fetching:    "正在获取同国家候选节点",
		Connecting:  "正在建立 OpenVPN 隧道",
		Verifying:   "正在验证出口国家 / 运营商",
		Waiting:     "等待可用节点（转发已阻断）",
		Stopped:     "已暂停 / 停止（转发已阻断）",
		Unavailable: "状态不可用（请检查网关服务）",
	}[status.State]
	if state == "" {
		state = "未知（暂未确认出口）"
	}
	verified := status.State == Ready && status.Exit != nil && net.ParseIP(status.Exit.IP) != nil && status.Exit.Country == status.Country
	if status.State == Ready && !verified {
		state = "状态异常（尚无有效的已验证出口）"
	}
	fmt.Fprintf(&out, " 连接状态：%s\n", state)
	exit := "暂无已验证出口"
	if verified {
		exit = status.Exit.IP
	}
	fmt.Fprintf(&out, " 出口 IP：%s\n", terminal.Text(exit))
	mode := "未知"
	switch status.Mode {
	case "sticky":
		mode = "尽量保持节点 / 故障自动重连"
	case "fixed":
		mode = "锁定当前节点"
	}
	fmt.Fprintf(&out, " 连接模式：%s\n", mode)
	if status.Node != "" {
		fmt.Fprintf(&out, " 当前节点：%s\n", terminal.Text(status.Node))
	}
	if verified && status.Exit.ISP != "" {
		fmt.Fprintf(&out, " 出口运营商：%s\n", terminal.Text(status.Exit.ISP))
	}
	if status.Reason != "" && status.State != Ready && status.State != Stopped {
		fmt.Fprintf(&out, " 状态说明：%s\n", terminal.Text(status.Reason))
	}
	_, err := output.Write(out.Bytes())
	return err
}
