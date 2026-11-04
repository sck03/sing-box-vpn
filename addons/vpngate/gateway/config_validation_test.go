package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func auditConfig(t *testing.T) Config {
	t.Helper()
	c := defaults()
	c.DataDir, c.RunDir = t.TempDir(), t.TempDir()
	c.ProxyUser, c.ProxyPass = "user", strings.Repeat("a", 32)
	return c
}

func TestConfigRejectsInvalidEndpointsAndRootDirectories(t *testing.T) {
	for _, name := range []string{"listen", "dns", "source", "probe", "geo", "data-root", "runtime-root"} {
		t.Run(name, func(t *testing.T) {
			c := auditConfig(t)
			switch name {
			case "listen":
				c.Listen = "127.0.0.1:65536"
			case "dns":
				c.DNS = "1.1.1.1:65536"
			case "source":
				c.Sources = []string{"https:///missing-host"}
			case "probe":
				c.ProbeURL = "https:///missing-host"
			case "geo":
				c.GeoURL = "https:///missing-host?q=%s"
			default:
				root := filepath.VolumeName(c.DataDir) + string(filepath.Separator)
				traversal := root + "child" + string(filepath.Separator) + ".."
				if name == "data-root" {
					c.DataDir = traversal
				} else {
					c.RunDir = traversal
				}
			}
			if err := c.validate(); err == nil {
				t.Fatal("accepted an unusable endpoint or root directory")
			}
		})
	}
}

func TestReadConfigRejectsTrailingAndOversizedJSON(t *testing.T) {
	c := auditConfig(t)
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string][]byte{
		"trailing":  append(bytes.Clone(data), []byte(` {"enabled":true}`)...),
		"oversized": append(bytes.Clone(data), bytes.Repeat([]byte(" "), 1<<20)...),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, input, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := readConfig(path); err == nil {
				t.Fatal("damaged configuration was accepted")
			}
		})
	}
}

type auditTransport func(*http.Request) (*http.Response, error)

func (f auditTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func auditResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestGeoURLPreservesPercentEncodedParameters(t *testing.T) {
	c := auditConfig(t)
	c.ProbeURL = "https://probe.example/"
	c.GeoURL = "https://geo.example/?q=%s&token=a%2Fb"
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	m := newManager(c, "")
	defer m.sourceClient.CloseIdleConnections()
	m.hostClient.Transport = auditTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "probe.example" {
			return auditResponse("198.51.100.1"), nil
		}
		if r.URL.RawQuery != "q=203.0.113.5&token=a%2Fb" {
			t.Errorf("encoded geolocation parameter was changed: %s", r.URL.RawQuery)
		}
		return auditResponse(`{"ip":"203.0.113.5","country_code":"JP","hosting":false}`), nil
	})
	client := &http.Client{Transport: auditTransport(func(*http.Request) (*http.Response, error) {
		return auditResponse("203.0.113.5"), nil
	})}
	if _, err := m.probe(context.Background(), c, &fakeTunnel{}, client); err != nil {
		t.Fatal(err)
	}
}
