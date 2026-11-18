package manage

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestAdministratorInputPreservesSecrets(t *testing.T) {
	want := administratorCredentials{Username: "operator", Password: "  中文 \"quotes\" \\ slash\t  "}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		data string
		nul  bool
	}{
		{"json", string(raw), false},
		{"installer", want.Username + "\x00" + want.Password + "\x00", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := readAdministrator(strings.NewReader(test.data), test.nul)
			if err != nil || got != want {
				t.Fatal("administrator fields changed", err)
			}
		})
	}
	for _, data := range []string{"admin\npassword", "admin\x00password", "admin\x00password\x00extra", strings.Repeat("x", 4097)} {
		if _, err := readAdministrator(strings.NewReader(data), true); err == nil {
			t.Fatal("accepted malformed or oversized administrator input")
		}
	}
	if _, err := readAdministrator(strings.NewReader(`{"username":"admin","password":"secret","extra":1}`), false); err == nil {
		t.Fatal("accepted unknown administrator field")
	}
}

func TestAdministratorStdinInitAndReset(t *testing.T) {
	root, core := t.TempDir(), coreBinary(t)
	args := []string{"--root", root, "--core", core, "--runtime", core, "--address", "127.0.0.1", "--no-system", "--admin-stdin"}
	password := "  安装 \"password\" \\  "
	if err := RunPanel(t.Context(), append(args, "init"), strings.NewReader("admin\x00"+password+"\x00"), io.Discard); err != nil {
		t.Fatal(err)
	}
	service, err := OpenPlatform(root, core, core, true)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err = service.Auth.Login(t.Context(), "admin", password, "", "local"); err != nil {
		t.Fatal("installer password did not authenticate", err)
	}
	next := "  重置 \"password\" \\  "
	if err = RunPanel(t.Context(), append(args, "reset-admin"), strings.NewReader("admin\x00"+next+"\x00"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Auth.Login(t.Context(), "admin", next, "", "local"); err != nil {
		t.Fatal("reset password did not authenticate", err)
	}
	if _, err = service.Auth.Login(t.Context(), "admin", password, "", "local"); err == nil {
		t.Fatal("old administrator password survived reset")
	}
	if err = RunPanel(t.Context(), append(args, "status"), new(bytes.Buffer), io.Discard); err == nil {
		t.Fatal("accepted administrator input for a read-only operation")
	}
}
