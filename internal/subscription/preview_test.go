package subscription

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestURIAddressConventions(t *testing.T) {
	for _, value := range []string{
		"HY2://alice:pass@example.com:4000-4002,5000/#hopping",
		"hy2://alice:pass@[2001:db8::1]:4000-4002,5000/#hopping",
	} {
		n, err := ParseURI(value)
		if err != nil || n.Port != 4000 || len(n.Ports) != 2 || n.Password != "alice:pass" {
			t.Fatal(n, err)
		}
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("aes-128-gcm:password@example.com:443"))
	n, err := ParseURI("ss://" + encoded + "#sample")
	if err != nil || n.Password != "password" {
		t.Fatal(n, err)
	}
	n, err = ParseURI("vless://00000000-0000-4000-8000-000000000001@example.com:443?type=ws&path=%2Fws%3Ftoken%3Dx%26ed%3D2048#ws")
	if err != nil || n.Transport.Path != "/ws?token=x" || n.Transport.EarlyData != 2048 {
		t.Fatal(n, err)
	}
}

func TestPreviewRedactsSecretsAndPreservesBoundaries(t *testing.T) {
	n := portableNodes()[0]
	for _, client := range []string{"sing-box", "mihomo", "uri", "base64", "surge", "loon", "quantumult-x"} {
		a, _, err := Render([]Node{n}, Template{Client: client, Version: 1, Mode: "nodes"}, false)
		if err != nil {
			t.Fatal(err)
		}
		preview, err := PreviewArtifact(a, client, 65536)
		if err != nil {
			t.Fatal(client, err)
		}
		if strings.Contains(preview.Content, n.Password) {
			t.Fatal("password leaked", client)
		}
	}
	a := Artifact{MIME: "application/yaml", Data: []byte(`{"obfs-password":"private-obfs","headers":{"Authorization":"private-header"},"url":"https://example.com/private"}`)}
	preview, err := PreviewArtifact(a, "mihomo", 32)
	if err != nil || !preview.Truncated || strings.Contains(preview.Content, "private") {
		t.Fatal(preview, err)
	}
}

func TestXHTTPWithInstalledMihomo(t *testing.T) {
	binary := os.Getenv("MIHOMO_BINARY")
	if binary == "" {
		t.Skip("Mihomo not configured")
	}
	n, err := ParseURI("vless://00000000-0000-4000-8000-000000000001@example.com:443?security=tls&type=xhttp&path=%2Fx&mode=packet-up#xhttp")
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := Render([]Node{n}, Template{Client: "mihomo", Version: 1, Mode: "config"}, false)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "xhttp.json")
	if err := os.WriteFile(path, a.Data, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(binary, "-t", "-d", root, "-f", path).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	var v Object
	_ = json.Unmarshal(a.Data, &v)
	if obj(v["proxies"].([]any)[0])["network"] != "xhttp" {
		t.Fatal("XHTTP transport lost")
	}
}
