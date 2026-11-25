package manage

import (
	"testing"
)

func TestExplicitDomainRoutingWithCore(t *testing.T) {
	for _, strategy := range []string{"prefer_ipv4", "prefer_ipv6"} {
		server, err := ServerConfig(nil, CoreSettings{Route: Object{"rules": []Object{{"action": "sniff"}, {"domain_suffix": []string{"example.com"}, "action": "resolve", "strategy": strategy}, {"domain_suffix": []string{"example.com"}, "action": "route", "outbound": "direct"}}, "final": "direct", "default_domain_resolver": "dns-local"}})
		if err != nil {
			t.Fatal(err)
		}
		if err = CheckJSON(t.Context(), coreBinary(t), t.TempDir(), server); err != nil {
			t.Fatal(err)
		}
	}
}
func TestDefaultServerSkipsUnneededSniffing(t *testing.T) {
	server, err := ServerConfig(nil, CoreSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if len(objects(object(server["route"])["rules"])) != 0 || len(objects(server["outbounds"])) != 1 {
		t.Fatal("default config contains unused routing", server)
	}
}
