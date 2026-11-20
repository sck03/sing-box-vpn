package vpngate

import (
	project "github.com/sck03/sing-box-vpn"
	"strings"
)

func version() string { return strings.TrimPrefix(project.Versions.Release, "v") }
