//go:build !with_wireguard

package runtime

import "github.com/sagernet/sing-box/adapter/endpoint"

func registerEndpoints(*endpoint.Registry) {}
