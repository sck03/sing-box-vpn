package manage

import "github.com/sck03/sing-box-vpn/internal/serverconfig"

func validateWebSocketPath(path string) error { return serverconfig.ValidateWebSocketPath(path) }
