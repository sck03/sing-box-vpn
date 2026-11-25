package vpngate

import "github.com/sck03/sing-box-vpn/internal/gatewayconfig"

func defaults() Config { return Config(gatewayconfig.Default()) }
