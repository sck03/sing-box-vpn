package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestGeoRejectsInvalidASN(t *testing.T) {
	for _, value := range []string{"0", "-1", "42.5", "4294967296", `"42"`, "null"} {
		for _, field := range []string{"asn", "connection"} {
			data := fmt.Sprintf(`{"country_code":"JP",%q:{"asn":%s,"type":"isp"}}`, field, value)
			if _, err := parseGeo([]byte(data), "203.0.113.5"); err == nil {
				t.Errorf("accepted invalid %s ASN %s", field, value)
			}
		}
	}
	for _, value := range []string{"42", "4294967295"} {
		data := fmt.Sprintf(`{"country_code":"JP","asn":{"asn":%s,"type":"isp"}}`, value)
		if _, err := parseGeo([]byte(data), "203.0.113.5"); err != nil {
			t.Errorf("rejected valid ASN %s: %v", value, err)
		}
	}
}

func TestConfigRejectsInvalidASNAllowlist(t *testing.T) {
	for _, value := range []int{0, -1, 1 << 32} {
		cfg := auditConfig(t)
		cfg.AllowASNs = []int{42, value}
		if err := cfg.validate(); err == nil {
			t.Errorf("accepted invalid allowlist ASN %d", value)
		}
	}
}

func TestProbeRejectsEquivalentHostExit(t *testing.T) {
	for _, pair := range [][2]string{
		{"203.0.113.5", "::ffff:203.0.113.5"},
		{"::ffff:203.0.113.5", "203.0.113.5"},
		{"::ffff:cb00:7105", "::ffff:203.0.113.5"},
	} {
		t.Run(strings.Join(pair[:], "/"), func(t *testing.T) {
			cfg := defaults()
			cfg.DataDir = t.TempDir()
			m := newManager(cfg, "")
			defer m.sourceClient.CloseIdleConnections()
			m.hostClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != cfg.ProbeURL {
					t.Error("same-address exit reached geolocation lookup")
					return testHTTPResponse(`{"country_code":"JP","hosting":false}`), nil
				}
				return testHTTPResponse(pair[1]), nil
			})
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return testHTTPResponse(pair[0]), nil
			})}
			if _, err := m.probe(context.Background(), cfg, &fakeTunnel{}, client); err == nil || !strings.Contains(err.Error(), "equals VPS exit") {
				t.Fatalf("equivalent addresses bypassed host exit rejection: %v", err)
			}
		})
	}
}

func TestProbeCanonicalizesExitAndGeoCache(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	lookups := 0
	m.hostClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == cfg.ProbeURL {
			return testHTTPResponse("203.0.113.6"), nil
		}
		lookups++
		if r.URL.Query().Get("q") != "203.0.113.5" {
			t.Errorf("geolocation used noncanonical address: %s", r.URL)
		}
		return testHTTPResponse(`{"ip":"203.0.113.5","country_code":"JP","hosting":false}`), nil
	})
	for _, address := range []string{"::ffff:cb00:7105", "203.0.113.5"} {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return testHTTPResponse(address), nil
		})}
		geo, err := m.probe(context.Background(), cfg, &fakeTunnel{}, client)
		if err != nil || geo.IP != "203.0.113.5" {
			t.Fatalf("unexpected normalized exit: %+v, %v", geo, err)
		}
	}
	if lookups != 1 {
		t.Fatalf("equivalent addresses caused %d geolocation requests", lookups)
	}
}
