package subscription

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

var fieldSchemas sync.Map

// Reject unrepresented connection options rather than silently producing a
// different connection. Full configuration sections outside nodes are ignored.
func nativeFields(m Object, stringsKeys, integers, booleans, objects, lists string) error {
	key := strings.Join([]string{stringsKeys, integers, booleans, objects, lists}, "\x00")
	stored, exists := fieldSchemas.Load(key)
	if !exists {
		kinds := map[string]byte{}
		for kind, fields := range map[byte]string{'s': stringsKeys, 'i': integers, 'b': booleans, 'o': objects, 'a': lists} {
			for _, name := range strings.Fields(fields) {
				kinds[name] = kind
			}
		}
		stored, _ = fieldSchemas.LoadOrStore(key, kinds)
	}
	kinds := stored.(map[string]byte)
	for key, value := range m {
		valid := false
		switch kinds[key] {
		case 's':
			_, valid = value.(string)
		case 'i':
			valid = value != nil && integer(value) >= 0
			switch value.(type) {
			case string, int, float64:
			default:
				valid = false
			}
		case 'b':
			_, valid = value.(bool)
		case 'o':
			_, valid = value.(map[string]any)
		case 'a':
			switch values := value.(type) {
			case []string:
				valid = true
			case []any:
				valid = true
				for _, v := range values {
					if _, ok := v.(string); !ok {
						valid = false
					}
				}
			}
		default:
			return fmt.Errorf("unsupported connection field %s", key)
		}
		if !valid {
			return fmt.Errorf("invalid type for connection field %s", key)
		}
	}
	return nil
}

func singBoxFields(m Object) error {
	if err := nativeFields(m, "type tag server uuid password flow security method congestion_control udp_relay_mode hop_interval heartbeat idle_session_check_interval idle_session_timeout plugin plugin_opts", "server_port alter_id min_idle_session", "zero_rtt_handshake", "tls transport obfs", "server_ports"); err != nil {
		return err
	}
	if integer(m["alter_id"]) != 0 || boolean(m["zero_rtt_handshake"]) {
		return errors.New("legacy VMess or TUIC zero RTT is unsupported")
	}
	for key, expected := range map[string]string{"heartbeat": "10s", "idle_session_check_interval": "30s", "idle_session_timeout": "30s"} {
		if value, ok := m[key]; ok && str(value) != expected {
			return fmt.Errorf("unsupported %s value", key)
		}
	}
	if value, ok := m["min_idle_session"]; ok && integer(value) != 1 {
		return errors.New("unsupported idle session count")
	}
	t := obj(m["tls"])
	if err := nativeFields(t, "server_name", "", "enabled insecure", "utls reality", "alpn certificate_public_key_sha256"); err != nil {
		return err
	}
	if err := nativeFields(obj(t["utls"]), "fingerprint", "", "enabled", "", ""); err != nil {
		return err
	}
	if u := obj(t["utls"]); u != nil && !boolean(u["enabled"]) {
		return errors.New("disabled uTLS options are unsupported")
	}
	if err := nativeFields(obj(t["reality"]), "public_key short_id", "", "enabled", "", ""); err != nil {
		return err
	}
	if r := obj(t["reality"]); r != nil && !boolean(r["enabled"]) {
		return errors.New("disabled Reality options are unsupported")
	}
	w := obj(m["transport"])
	if err := nativeFields(w, "type path host service_name early_data_header_name", "max_early_data", "", "headers", ""); err != nil {
		return err
	}
	if err := nativeFields(obj(w["headers"]), "Host", "", "", "", ""); err != nil {
		return err
	}
	if str(w["type"]) != "grpc" && str(w["service_name"]) != "" || str(w["type"]) != "httpupgrade" && str(w["host"]) != "" || str(w["type"]) == "httpupgrade" && len(obj(w["headers"])) > 0 {
		return errors.New("transport options do not match transport type")
	}
	return nativeFields(obj(m["obfs"]), "type password", "", "", "", "")
}

func mihomoFields(m Object) error {
	if err := nativeFields(m, "name type server uuid password cipher flow congestion-controller udp-relay-mode servername sni fingerprint client-fingerprint network ports obfs obfs-password plugin", "port alterId hop-interval idle-session-check-interval idle-session-timeout min-idle-session", "tls skip-cert-verify udp", "reality-opts ws-opts grpc-opts plugin-opts xhttp-opts", "alpn"); err != nil {
		return err
	}
	if value, ok := m["udp"]; ok && !boolean(value) {
		return errors.New("disabling UDP is not represented by the node model")
	}
	for key, expected := range map[string]int{"idle-session-check-interval": 30, "idle-session-timeout": 30, "min-idle-session": 1} {
		if value, ok := m[key]; ok && integer(value) != expected {
			return fmt.Errorf("unsupported %s value", key)
		}
	}
	if err := nativeFields(obj(m["reality-opts"]), "public-key short-id", "", "", "", ""); err != nil {
		return err
	}
	w := obj(m["ws-opts"])
	if len(w) > 0 && str(m["network"]) != "ws" && str(m["network"]) != "websocket" || len(obj(m["grpc-opts"])) > 0 && str(m["network"]) != "grpc" {
		return errors.New("transport options do not match network")
	}
	if err := nativeFields(w, "path early-data-header-name", "max-early-data", "v2ray-http-upgrade", "headers", ""); err != nil {
		return err
	}
	if err := nativeFields(obj(w["headers"]), "Host", "", "", "", ""); err != nil {
		return err
	}
	if err := nativeFields(obj(m["grpc-opts"]), "grpc-service-name", "", "", "", ""); err != nil {
		return err
	}
	if err := nativeFields(obj(m["plugin-opts"]), "mode host path", "", "tls mux", "", ""); err != nil {
		return err
	}
	if len(obj(m["xhttp-opts"])) > 0 && str(m["network"]) != "xhttp" {
		return errors.New("XHTTP options require XHTTP network")
	}
	_, err := xhttpFromNative(obj(m["xhttp-opts"]))
	return err
}
