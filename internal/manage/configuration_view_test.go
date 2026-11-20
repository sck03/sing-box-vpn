package manage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSingleProtocolPresetDoesNotAllocateOtherProtocols(t *testing.T) {
	for _, protocol := range Protocols {
		root := t.TempDir()
		used := map[int]bool{}
		profile, err := newProfile(root, used, "", []string{protocol})
		if err != nil {
			t.Fatal(err)
		}
		if len(profile.Ports) != 1 || len(used) != 1 {
			t.Fatal("unused protocol allocations", profile.Ports, used)
		}
		if protocol == "vless" || protocol == "vmess" {
			if _, err := os.Stat(filepath.Join(root, "cert.pem")); !os.IsNotExist(err) {
				t.Fatal("unnecessary certificate", err)
			}
		}
	}
}

func BenchmarkConfigurationView(b *testing.B) {
	config := PlatformConfig{Schema: PlatformSchema, App: App{Name: "view", Address: "example.com"}}
	for i := 0; i < 128; i++ {
		config.App.Advanced.Outbounds = append(config.App.Advanced.Outbounds, Object{"type": "anytls", "password": "fixture-secret", "tls": Object{"enabled": true, "server_name": "example.com"}})
	}
	data, err := encodePlatform(config)
	if err != nil {
		b.Fatal(err)
	}
	for _, old := range []bool{true, false} {
		name := "current"
		if old {
			name = "previous"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var result PlatformConfig
				if old {
					result, err = decodePlatform(data)
					if err != nil {
						b.Fatal(err)
					}
					raw, _ := json.Marshal(result)
					var object any
					if err = json.Unmarshal(raw, &object); err != nil {
						b.Fatal(err)
					}
					redactPlatform(object)
					raw, _ = json.Marshal(object)
					err = json.Unmarshal(raw, &result)
				} else {
					result, err = configurationView(data, false)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestConfigurationViewPreservesTypedShapeAndRedacts(t *testing.T) {
	config := PlatformConfig{Schema: PlatformSchema, App: App{Nodes: []ManagedNode{{TLS: ManagedTLS{RealityPrivate: "private-reality"}}}, Advanced: CoreSettings{Outbounds: []Object{{"type": "anytls", "password": "private-password", "tls": Object{"enabled": true}}}}}}
	data, _ := encodePlatform(config)
	view, err := configurationView(data, false)
	if err != nil {
		t.Fatal(err)
	}
	redacted, _ := json.Marshal(view)
	if bytes.Contains(redacted, []byte("private-")) || !bytes.Contains(redacted, []byte("[redacted]")) {
		t.Fatal("secret projection failed")
	}
	if !bytes.Contains(data, []byte("private-password")) {
		t.Fatal("source was mutated")
	}
}
