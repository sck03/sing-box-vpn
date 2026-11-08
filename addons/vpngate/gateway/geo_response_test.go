package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestGeoRejectsRoundedASN(t *testing.T) {
	for _, value := range []string{"42.00000000000000001", "4294967295.0000001"} {
		for _, field := range []string{"asn", "connection"} {
			data := fmt.Sprintf(`{"country_code":"JP",%q:{"asn":%s,"type":"isp"}}`, field, value)
			if g, err := parseGeo([]byte(data), "203.0.113.5"); err == nil {
				t.Errorf("fractional ASN %s accepted as %d", value, g.ASN)
			}
		}
	}
}

func TestGeoProviderResponses(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       Geo
	}{
		{"ipapi", `{"ip":"203.0.113.5","is_datacenter":false,"location":{"country_code":"jp"},"asn":{"asn":42,"org":"Example ISP","type":"isp"}}`, Geo{Country: "JP", ASN: 42, ISP: "Example ISP", ASNType: "isp", HostingKnown: true}},
		{"connection", `{"query":"::ffff:203.0.113.5","countryCode":"JP","hosting":false,"connection":{"asn":4294967295,"isp":"Example ISP","type":"ISP"}}`, Geo{Country: "JP", ASN: 4294967295, ISP: "Example ISP", ASNType: "isp", HostingKnown: true}},
		{"hosting wins", `{"country":"JP","is_datacenter":true,"hosting":false,"isp":"Example","asn":{"type":"isp"}}`, Geo{Country: "JP", ISP: "Example", ASNType: "isp", Hosting: true, HostingKnown: true}},
		{"unknown hosting", `{"country_code":"JP","success":true,"error":false}`, Geo{Country: "JP", ISP: "AS0"}},
		{"hosting type", `{"country_code":"JP","asn":{"asn":42,"type":"hosting"}}`, Geo{Country: "JP", ASN: 42, ISP: "AS42", ASNType: "hosting", Hosting: true, HostingKnown: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.want.IP = "203.0.113.5"
			got, err := parseGeo([]byte(test.body), test.want.IP)
			if err != nil || got != test.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, test.want)
			}
		})
	}
}

func TestGeoRejectsInvalidProviderResponse(t *testing.T) {
	for _, fields := range []string{
		`"success":false`, `"error":"quota exceeded"`, `"error":{"message":"unavailable"}`,
		`"ip":null`, `"query":42`, `"ip":"203.0.113.6"`,
		`"is_datacenter":"true"`, `"hosting":1`, `"asn":{"type":true}`,
	} {
		t.Run(fields, func(t *testing.T) {
			if _, err := parseGeo([]byte(`{"country_code":"JP",`+fields+`}`), "203.0.113.5"); err == nil {
				t.Fatal("accepted invalid provider response")
			}
		})
	}
	for _, value := range []string{"null", "false", `""`} {
		if _, err := parseGeo([]byte(`{"country_code":"JP","error":`+value+`}`), "203.0.113.5"); err != nil {
			t.Fatalf("rejected empty provider error %s: %v", value, err)
		}
	}
}

func TestProbeDoesNotCacheInvalidPolicyMetadata(t *testing.T) {
	cfg := auditConfig(t)
	cfg.RequireISP, cfg.ExcludeHosting, cfg.AllowASNs = true, true, []int{42}
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	metadata := ""
	lookups := 0
	m.hostClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == cfg.ProbeURL {
			return testHTTPResponse("203.0.113.6"), nil
		}
		lookups++
		return testHTTPResponse(metadata), nil
	})
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testHTTPResponse("203.0.113.5"), nil
	})}
	for _, fields := range []string{
		`"asn":{"asn":42.00000000000000001,"type":"isp"}`,
		`"is_datacenter":"true","asn":{"asn":42,"type":"isp"}`,
	} {
		metadata = `{"country_code":"JP",` + fields + `}`
		_, err := m.probe(context.Background(), cfg, &fakeTunnel{}, client)
		var unavailable probeUnavailable
		if !errors.As(err, &unavailable) || len(m.geoCache) != 0 {
			t.Fatalf("invalid policy metadata accepted or cached: %v, cache size %d", err, len(m.geoCache))
		}
	}
	metadata = `{"country_code":"JP","is_datacenter":false,"asn":{"asn":42,"type":"isp"}}`
	if _, err := m.probe(context.Background(), cfg, &fakeTunnel{}, client); err != nil || lookups != 3 || len(m.geoCache) != 1 {
		t.Fatalf("corrected metadata did not recover: %v, lookups %d, cache size %d", err, lookups, len(m.geoCache))
	}
}

func BenchmarkGeoResponse(b *testing.B) {
	base := `"ip":"203.0.113.5","is_datacenter":false,"location":{"country_code":"JP"},"asn":{"asn":42,"org":"Example ISP","type":"isp"}`
	for _, test := range []struct{ name, body string }{
		{"Typical", "{" + base + "}"},
		{"UnusedMetadata", "{" + base + `,"extra":[` + strings.Repeat(`{"label":"unused","score":42},`, 8191) + `null]}`},
	} {
		b.Run(test.name, func(b *testing.B) {
			data := []byte(test.body)
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for b.Loop() {
				if _, err := parseGeo(data, "203.0.113.5"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
