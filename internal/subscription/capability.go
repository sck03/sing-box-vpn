package subscription

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sck03/sing-box-vpn/internal/clientprofile"
)

func capabilityError(n Node, client string, cap clientprofile.Profile) error {
	connection, ok := clientprofile.ConnectionFor(client, n.Protocol)
	if !ok {
		return fmt.Errorf("%s does not support %s", client, n.Protocol)
	}
	if !slices.Contains(connection.Transports, fallback(n.Transport.Type, "tcp")) {
		return errors.New("target adapter cannot preserve this transport")
	}
	if len(connection.Ciphers) > 0 && !slices.Contains(connection.Ciphers, fallback(n.Cipher, "auto")) {
		return errors.New("target adapter cannot preserve this cipher")
	}
	if n.Plugin != nil && !slices.Contains(connection.Plugins, n.Plugin.Type) {
		return errors.New("target adapter cannot preserve this SIP003 plugin")
	}
	if n.Transport.Type == "grpc" && (!slices.Contains(connection.GRPCModes, fallback(n.Transport.Mode, "gun")) || n.Transport.Authority != "" && !connection.GRPCAuthority) {
		return errors.New("target adapter cannot preserve gRPC mode or authority")
	}
	if (n.Transport.EarlyData != 0 || n.Transport.EarlyDataHeader != "") && !connection.EarlyData {
		return errors.New("target adapter cannot preserve WebSocket early data")
	}
	if n.Transport.XHTTP != nil && !connection.XHTTPExtra {
		return errors.New("target adapter cannot preserve XHTTP extra options")
	}
	if n.TLS.RealityKey != "" && (client == "stash" && n.Transport.Type != "" && n.Transport.Type != "tcp" || (client == "surge" || client == "surge-mac") && n.Transport.Type == "ws") {
		return errors.New("target adapter cannot combine Reality with this transport")
	}
	if n.Obfs != "" && !cap.Features.HysteriaObfs {
		return errors.New("target adapter cannot preserve Hysteria2 obfuscation")
	}
	if len(n.TLS.PublicKeyPins) > 0 && n.TLS.CertificateHash == "" && client != "sing-box" {
		return errors.New("target cannot preserve public-key certificate pin")
	}
	uri := slices.Contains([]string{"uri", "base64", "shadowrocket"}, client)
	// URI formats can carry a Hysteria2 hash; other URI protocols explicitly
	// report their import limitation below. Native adapters never drop a pin.
	if n.TLS.CertificateHash != "" && !cap.Features.CertificatePin && client != "sing-box" && !(uri && (n.Protocol == "hysteria2" || n.TLS.Insecure)) {
		return errors.New("target cannot preserve certificate verification pin")
	}
	if len(n.Ports) > 0 && !cap.Features.PortHopping {
		return errors.New("target adapter cannot preserve port hopping")
	}
	if n.HopIntervalSeconds != 0 && !cap.Features.HopInterval {
		return errors.New("target adapter cannot preserve port hopping interval")
	}
	if n.TLS.Fingerprint != "" && !connection.UTLS {
		return errors.New("target adapter cannot preserve the requested TLS client fingerprint")
	}
	if client == "egern" && len(n.TLS.ALPN) > 0 && n.Protocol != "tuic" {
		return errors.New("Egern adapter cannot preserve ALPN for this protocol")
	}
	if client == "egern" && n.Transport.Type == "grpc" && !n.TLS.Enabled {
		return errors.New("Egern gRPC transport requires TLS")
	}
	if n.Protocol == "tuic" && (n.Congestion != "" || n.UDPRelay != "") && slices.Contains([]string{"egern", "surge", "surge-mac", "surfboard"}, client) {
		return errors.New("target adapter cannot preserve explicit TUIC congestion or UDP relay settings")
	}
	if client == "sing-box" && n.TLS.CertificateHash != "" && len(n.TLS.PublicKeyPins) == 0 {
		return errors.New("sing-box requires a public-key pin, not a certificate hash")
	}
	if !slices.Contains([]string{"sing-box", "mihomo", "openclash", "stash", "egern", "uri", "base64", "shadowrocket"}, client) {
		if textCommentName(n.Name) {
			return errors.New("target text format cannot represent a comment-prefixed node name; rename the node")
		}
		fields := []string{n.Name, n.Server, n.Password, n.Transport.Path, n.Transport.Host, n.Transport.Authority, n.Transport.ServiceName, n.TLS.ServerName}
		fields = append(fields, n.TLS.ALPN...)
		if n.Plugin != nil {
			fields = append(fields, n.Plugin.Host, n.Plugin.Path)
		}
		for _, field := range fields {
			if strings.ContainsAny(field, ",\"=\r\n|[]") {
				return errors.New("target text format cannot safely represent a field delimiter")
			}
		}
	}
	return nil
}

func textCommentName(name string) bool {
	name = strings.TrimSpace(name)
	return strings.HasPrefix(name, "#") || strings.HasPrefix(name, ";") || strings.HasPrefix(name, "//")
}
