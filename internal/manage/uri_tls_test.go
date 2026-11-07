package manage

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestHysteriaURIPinsOnlySelfSignedCertificate(t *testing.T) {
	root := t.TempDir()
	cert := filepath.Join(root, "cert.pem")
	if err := EnsureCertificate(cert, filepath.Join(root, "key.pem"), "example.com"); err != nil {
		t.Fatal(err)
	}
	info, err := (certificateCache{}).get(cert)
	if err != nil {
		t.Fatal(err)
	}
	node := Object{"type": "hysteria2", "tag": "test", "server": "2001:db8::1", "server_port": 443, "password": "空 格@:/?#%", "tls": Object{"enabled": true, "server_name": "example.com"}}
	for _, selfSigned := range []bool{true, false} {
		profile := Profile{SelfSigned: selfSigned, Certificate: cert, Hopping: []string{"31000:31100", "31200"}}
		if !selfSigned {
			// CA-backed links must not depend on a leaf certificate that changes
			// during renewal. No file read is needed for this export.
			profile.Certificate = filepath.Join(root, "unused.pem")
		}
		link, err := URI(node, profile)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(link)
		if err != nil {
			t.Fatal(err)
		}
		wantPin, wantInsecure := "", "0"
		if selfSigned {
			wantPin, wantInsecure = info.Hash, "1"
		}
		if u.Query().Get("pinSHA256") != wantPin || u.Query().Get("insecure") != wantInsecure {
			t.Fatalf("wrong certificate policy (self-signed=%v): %v", selfSigned, u.Query())
		}
		if u.User.Username() != node["password"] || u.Hostname() != "2001:db8::1" || u.Query().Get("mport") != "31000-31100,31200" {
			t.Fatal("URI lost password, IPv6 address or hopping ports")
		}
	}
}

func TestVMessURISelfSignedTLS(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, selfSigned := range []bool{false, true} {
			node, profile := vmessURIFixture()
			object(node["tls"])["enabled"], profile.SelfSigned = enabled, selfSigned
			link, err := URI(node, profile)
			if err != nil {
				t.Fatal(err)
			}
			data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(link, "vmess://"))
			if err != nil {
				t.Fatal(err)
			}
			var value map[string]string
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			want := ""
			if enabled && selfSigned {
				want = "1"
			}
			if value["insecure"] != want {
				t.Fatalf("TLS=%v self-signed=%v: insecure=%q, want %q", enabled, selfSigned, value["insecure"], want)
			}
		}
	}
}
