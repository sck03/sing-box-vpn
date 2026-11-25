package manage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitOutputsKeepCountryIsolation(t *testing.T) {
	root, core, app := appFixture(t)
	before := readArtifact(t, root, "normal-uri")
	if _, err := prepareFixtureCountry(t.Context(), root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, readArtifact(t, root, "normal-uri")) {
		t.Fatal("country changed ordinary outputs")
	}
	if len(readArtifact(t, root, "vpngate-uri")) == 0 {
		t.Fatal("missing country output")
	}
	for _, name := range []string{"sbox.json", "clmi.yaml", "proxies.yaml", "jhsub.txt", "jhsub-base64.txt", "share.json"} {
		for _, prefix := range []string{"", "vpngate"} {
			if _, err := os.Stat(filepath.Join(root, prefix, name)); !os.IsNotExist(err) {
				t.Fatal("obsolete export emitted", prefix, name, err)
			}
		}
	}
}
func TestRemovingCountryKeepsOrdinaryOutput(t *testing.T) {
	root, core, app := appFixture(t)
	if _, err := prepareFixtureCountry(t.Context(), root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	c, err := fixtureConfig(root, app)
	if err != nil {
		t.Fatal(err)
	}
	before := readArtifact(t, root, "normal-uri")
	c.VPN.ActiveCountry = ""
	c.Gateway = Object{}
	if err = publishFixtureConfig(t.Context(), root, core, c); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, readArtifact(t, root, "normal-uri")) {
		t.Fatal("removed ordinary output")
	}
}
