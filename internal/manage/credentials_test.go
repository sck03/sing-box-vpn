package manage

import (
	"bytes"
	"context"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const customUUID = "01234567-89ab-4cde-8f01-23456789abcd"

func TestUpgradeRepairsStaleCredentialMetadata(t *testing.T) {
	root, core, app := appFixture(t)
	server := readBytes(t, filepath.Join(root, "sb.json"))
	links := readBytes(t, filepath.Join(root, "jhsub.txt"))
	expected := text(objects(app.Inbounds[0]["users"])[0]["uuid"])
	app.Profile.Secrets.UUID["vless"] = randomUUID()
	if err := AtomicJSON(filepath.Join(root, "app.json"), app); err != nil {
		t.Fatal(err)
	}
	if err := RunConfig(context.Background(), []string{"--root", root, "--core", core, "init"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadJSON(filepath.Join(root, "app.json"), &app); err != nil {
		t.Fatal(err)
	}
	if app.Profile.Secrets.UUID["vless"] != expected || !bytes.Equal(server, readBytes(t, filepath.Join(root, "sb.json"))) || !bytes.Equal(links, readBytes(t, filepath.Join(root, "jhsub.txt"))) {
		t.Fatal("upgrade did not repair metadata while preserving existing nodes")
	}
}

func TestCustomCredentialsPersistAndStayScoped(t *testing.T) {
	root, core, app := appFixture(t)
	ctx := context.Background()
	if _, err := PrepareVPN(ctx, root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	beforeVPN := readBytes(t, filepath.Join(root, "vpngate", "server.json"))
	run := func(scope string, args ...string) error {
		return RunConfig(ctx, append(append([]string{"--root", root, "--core", core}, args...), "--scope", scope), &bytes.Buffer{})
	}
	if err := run(normalScope, "credentials", "all", strings.ToUpper(customUUID)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeVPN, readBytes(t, filepath.Join(root, "vpngate", "server.json"))) {
		t.Fatal("ordinary credentials changed the gateway entries")
	}
	var current App
	if _, err := ReadJSON(filepath.Join(root, "app.json"), &current); err != nil {
		t.Fatal(err)
	}
	for _, in := range current.Inbounds {
		kind, users := text(in["type"]), objects(in["users"])
		for _, key := range []string{"uuid", "password"} {
			if value, exists := users[0][key]; exists && value != customUUID {
				t.Fatal("custom credential missing from an inbound", kind, key)
			}
		}
	}
	for _, value := range current.Profile.Secrets.UUID {
		if value != customUUID {
			t.Fatal("stored UUID and running configuration disagree")
		}
	}
	for _, value := range current.Profile.Secrets.Passwords {
		if value != customUUID {
			t.Fatal("stored password and running configuration disagree")
		}
	}
	ordinary := readBytes(t, filepath.Join(root, "sb.json"))
	_, oldSecrets, err := loadVPN(root)
	if err != nil {
		t.Fatal(err)
	}
	old := oldSecrets["JP"]
	password := "独立 TUIC 密码 @:/?#%"
	if err := run(vpngateScope, "credentials", "tuic", customUUID, password); err != nil {
		t.Fatal(err)
	}
	_, secrets, err := loadVPN(root)
	if err != nil {
		t.Fatal(err)
	}
	next := secrets["JP"]
	if next.UUID["tuic"] != customUUID || next.Passwords["tuic"] != password || next.SocksPass != old.SocksPass || next.SocksUser != old.SocksUser || next.UUID["vless"] != old.UUID["vless"] {
		t.Fatal("customizing TUIC changed unrelated credentials")
	}
	if !bytes.Equal(ordinary, readBytes(t, filepath.Join(root, "sb.json"))) {
		t.Fatal("gateway credentials changed ordinary entries")
	}
	for _, line := range strings.Split(string(readBytes(t, filepath.Join(root, "vpngate", "jhsub.txt"))), "\n") {
		if strings.HasPrefix(line, "tuic://") {
			link, err := url.Parse(line)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := link.User.Password()
			if got != password || link.User.Username() != customUUID {
				t.Fatal("TUIC URI did not preserve the custom credentials")
			}
		}
	}
	if err := run(normalScope, "reset", "tuic"); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadJSON(filepath.Join(root, "app.json"), &current); err != nil {
		t.Fatal(err)
	}
	if current.Profile.Secrets.UUID["tuic"] == customUUID || current.Profile.Secrets.Passwords["tuic"] == customUUID || current.Profile.Secrets.UUID["vless"] != customUUID {
		t.Fatal("random reset did not update only the selected protocol")
	}
}

func TestInvalidCredentialsLeaveConfigurationIntact(t *testing.T) {
	root, core, _ := appFixture(t)
	before := readBytes(t, filepath.Join(root, "app.json"))
	for _, values := range [][]string{{"all", "not-a-uuid"}, {"tuic", "", ""}, {"tuic", customUUID, "a\nb"}, {"anytls", strings.Repeat("a", 1025)}, {"vmess", customUUID, "extra"}, {"unknown", customUUID}} {
		err := RunConfig(context.Background(), append([]string{"--root", root, "--core", core, "credentials"}, values...), &bytes.Buffer{})
		if err == nil || !bytes.Equal(before, readBytes(t, filepath.Join(root, "app.json"))) {
			t.Fatal("invalid credentials were committed", values[0], err)
		}
	}
}

func TestCredentialInputAndImportedUsers(t *testing.T) {
	values, err := readCredentials(strings.NewReader(customUUID + "\nseparate password\n"))
	if err != nil || !reflect.DeepEqual(values, []string{customUUID, "separate password"}) {
		t.Fatal("stdin credentials were not parsed", err)
	}
	values, err = readCredentials(strings.NewReader(customUUID + "\r\nseparate password\r\n"))
	if err != nil || !reflect.DeepEqual(values, []string{customUUID, "separate password"}) {
		t.Fatal("CRLF input changed the credentials", err)
	}
	for _, input := range []string{"a\nb\nc", strings.Repeat("a", 2050)} {
		if _, err := readCredentials(strings.NewReader(input)); err == nil {
			t.Fatal("unbounded or multiline credentials accepted")
		}
	}
	old := randomUUID()
	app := App{Inbounds: []Object{{"type": "vmess", "users": []Object{{"uuid": randomUUID()}, {"uuid": old}}}}}
	if err := changeCredentials(&app, "credentials", []string{"vmess", customUUID}); err != nil {
		t.Fatal(err)
	}
	users := objects(app.Inbounds[0]["users"])
	if users[0]["uuid"] != customUUID || users[1]["uuid"] != old || app.Profile.Secrets.UUID["vmess"] != customUUID {
		t.Fatal("customization overwrote an imported additional user")
	}
}

func TestTUICCredentialsCanChangeIndependently(t *testing.T) {
	const oldUUID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	const oldPassword = "existing password"
	for _, tc := range []struct {
		name           string
		input          []string
		uuid, password string
	}{
		{"uuid only", []string{customUUID}, customUUID, oldPassword},
		{"blank password", []string{customUUID, ""}, customUUID, oldPassword},
		{"password only", []string{"", "  密码 @:/?#%  "}, oldUUID, "  密码 @:/?#%  "},
		{"both", []string{strings.ToUpper(customUUID), "new password"}, customUUID, "new password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := App{Inbounds: []Object{{"type": "tuic", "users": []Object{{"uuid": oldUUID, "password": oldPassword}}}}}
			if err := changeCredentials(&app, "credentials", append([]string{"tuic"}, tc.input...)); err != nil {
				t.Fatal(err)
			}
			user := objects(app.Inbounds[0]["users"])[0]
			if user["uuid"] != tc.uuid || user["password"] != tc.password || app.Profile.Secrets.UUID["tuic"] != tc.uuid || app.Profile.Secrets.Passwords["tuic"] != tc.password {
				t.Fatal("independent credential update changed the omitted field or lost metadata")
			}
		})
	}
}

func TestAllCredentialsAllowSeparatePassword(t *testing.T) {
	root, core, app := appFixture(t)
	const password = "  统一密码 @:/?#%  "
	if err := RunConfig(context.Background(), []string{"--root", root, "--core", core, "credentials", "all", customUUID, password}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadJSON(filepath.Join(root, "app.json"), &app); err != nil {
		t.Fatal(err)
	}
	client := readObject(t, filepath.Join(root, "sbox.json"))
	for _, entries := range [][]Object{app.Inbounds, objects(client["outbounds"])} {
		for _, entry := range entries {
			user := entry
			if users := objects(entry["users"]); len(users) > 0 {
				user = users[0]
			}
			if value, ok := user["uuid"]; ok && value != customUUID {
				t.Fatal("UUID not propagated to server/client")
			}
			if value, ok := user["password"]; ok && value != password {
				t.Fatal("password not propagated to server/client")
			}
		}
	}
}
