package manage

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWebSocketRejectsUnusablePathsBeforeMutation(t *testing.T) {
	for _, path := range []string{"", "ws", "/ws?ed=2048", "/ws?", "/ws#fragment", "/ws%2Fchild", "/ws%20child", "/ws\\child", "/ws\tchild", "/ws\x00child", "/ws child", "/ws\nchild", "/ws\u00a0child", "/ws\xff", "/" + strings.Repeat("x", 255)} {
		t.Run(path, func(t *testing.T) {
			app := App{Profile: Profile{WSPath: "/before"}, Inbounds: []Object{{"type": "vmess", "transport": Object{"path": "/before"}}}}
			before := clone(app.Inbounds)
			if err := mutateApp(&app, VPNSettings{}, "ws", []string{path}); err == nil {
				t.Fatal("accepted a WebSocket path that cannot be shared reliably")
			}
			if app.Profile.WSPath != "/before" || !reflect.DeepEqual(app.Inbounds, before) {
				t.Fatal("invalid WebSocket path changed configuration")
			}
		})
	}
}

func TestWebSocketPathsSharedByBothScopes(t *testing.T) {
	for _, path := range []string{"/", "/ws-._~", "/普通/日本🚀", "/" + strings.Repeat("x", 254)} {
		for _, scope := range []string{normalScope, vpngateScope} {
			t.Run(scope+path, func(t *testing.T) {
				profile := Profile{Ports: map[string]int{}, WSPath: "/before", Secrets: Secrets{UUID: map[string]string{}, Passwords: map[string]string{}}}
				app := App{Profile: profile, Inbounds: MakeInbounds(profile, "")}
				settings := VPNSettings{ActiveCountry: "JP", Profiles: map[string]Profile{"JP": profile}}
				if scope == vpngateScope {
					if err := mutateVPN(app, settings, map[string]Secrets{"JP": profile.Secrets}, "ws", []string{path}); err != nil {
						t.Fatal(err)
					}
					if settings.Profiles["JP"].WSPath != path || app.Profile.WSPath != "/before" {
						t.Fatal("VPNGate path did not remain independent")
					}
				} else {
					if err := mutateApp(&app, settings, "ws", []string{path}); err != nil {
						t.Fatal(err)
					}
					if app.Profile.WSPath != path || object(app.Inbounds[1]["transport"])["path"] != path {
						t.Fatal("ordinary WebSocket path changed during mutation")
					}
				}
			})
		}
	}
}

func TestWebSocketRejectsInvalidStoredPathBeforePublish(t *testing.T) {
	root, core, app := appFixture(t)
	before := readBytes(t, filepath.Join(root, "app.json"))
	for _, inbound := range app.Inbounds {
		if inbound["type"] == "vmess" {
			object(inbound["transport"])["path"] = "/ws?ed=2048"
		}
	}
	if err := CommitApp(context.Background(), root, core, &app); err == nil {
		t.Fatal("published an unusable WebSocket subscription")
	}
	if string(before) != string(readBytes(t, filepath.Join(root, "app.json"))) {
		t.Fatal("invalid WebSocket configuration was published")
	}
}
