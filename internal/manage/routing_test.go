package manage

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestDomainRoutingWithStableCore(t *testing.T) {
	core := coreBinary(t)
	for _, channel := range []string{"socks", "direct4", "direct6"} {
		t.Run(channel, func(t *testing.T) {
			server, err := ServerConfig(nil, map[string][]string{channel: {"example.com"}}, nil, 40000)
			if err != nil {
				t.Fatal(err)
			}
			if err := CheckJSON(context.Background(), core, t.TempDir(), server); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDefaultServerSkipsUnneededSniffing(t *testing.T) {
	server, err := ServerConfig(nil, nil, nil, 40000)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range objects(object(server["route"])["rules"]) {
		if rule["action"] == "sniff" {
			t.Fatal("server without domain routes still buffers and sniffs every connection")
		}
	}
}

func TestDomainValidationAndRouteNormalization(t *testing.T) {
	for _, domain := range []string{"example..com", "example.-com", "a-.example", strings.Repeat("a", 64) + ".com", ".example.com"} {
		if err := validateDomain(domain); err == nil {
			t.Fatalf("accepted invalid DNS name %q", domain)
		}
	}
	for _, domain := range []string{"example.com", "EXAMPLE.com", "xn--fiqs8s.cn", "localhost", strings.Repeat("a", 63) + ".com"} {
		if err := validateDomain(domain); err != nil {
			t.Fatalf("rejected valid DNS name %q: %v", domain, err)
		}
	}
	app := App{}
	if err := mutateApp(&app, VPNSettings{}, "route", []string{"socks", ".EXAMPLE.com", "example.com", "other.com"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(app.Routing["socks"], []string{"example.com", "other.com"}) {
		t.Fatal("duplicate domain suffixes retained", app.Routing)
	}
}
