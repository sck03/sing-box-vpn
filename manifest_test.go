package project

import (
	"os"
	"strings"
	"testing"
)

func TestManifestMatchesEmbeddedComponents(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, dependency := range []string{"github.com/sagernet/sing-box v" + Versions.SingBox.Tested, "github.com/go-acme/lego/v5 v" + Versions.CertificateManager} {
		if !strings.Contains(string(data), dependency+"\n") {
			t.Fatal("component manifest does not match go.mod", dependency)
		}
	}
	if !strings.HasPrefix(Versions.ManagedRuntime, Versions.SingBox.Tested+"+sbvpn.") {
		t.Fatal("runtime is not paired with the pinned sing-box dependency")
	}
}
