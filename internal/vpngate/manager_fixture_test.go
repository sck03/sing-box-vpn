package vpngate

import "github.com/sck03/sing-box-vpn/internal/manage"

func newTestManager(c Config, path string) *manager {
	return newManager(c, path, manage.ConfigTransaction)
}
