package publicaccess

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransportPolicy(t *testing.T) {
	tests := []struct {
		o  Options
		r  Rules
		ok bool
	}{
		{Options{Listen: "0.0.0.0:2097", PublicURL: "https://sub.example.test"}, Rules{RequireHTTPS: true}, false},
		{Options{Listen: "127.0.0.1:2097", PublicURL: "https://sub.example.test"}, Rules{RequireHTTPS: true}, false},
		{Options{Listen: "127.0.0.1:2096"}, Rules{}, true},
		{Options{Listen: "0.0.0.0:2096"}, Rules{}, false},
		{Options{Listen: "127.0.0.1:2097", PublicURL: "https://sub.example.test/prefix", TrustedProxy: []string{"127.0.0.1/32"}}, Rules{RequireHTTPS: true, AllowProxyPath: true}, true},
		{Options{Listen: "127.0.0.1:2096", PublicURL: "https://panel.example.test/prefix", TrustedProxy: []string{"127.0.0.1/32"}}, Rules{}, false},
	}
	for _, v := range tests {
		if err := v.o.Validate(v.r); (err == nil) != v.ok {
			t.Errorf("%+v: %v", v.o, err)
		}
	}
}
func TestProxyProtection(t *testing.T) {
	o := Options{Listen: "127.0.0.1:2097", PublicURL: "https://sub.example.test", TrustedProxy: []string{"127.0.0.1/32"}}
	h := o.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, v := range []struct {
		peer, host, proto string
		want              int
	}{{"127.0.0.1:1000", "sub.example.test", "https", 204}, {"192.0.2.1:1000", "sub.example.test", "https", 403}, {"127.0.0.1:1000", "sub.example.test", "http", 403}, {"127.0.0.1:1000", "other.example.test", "https", 400}} {
		r := httptest.NewRequest("GET", "http://"+v.host+"/user", nil)
		r.RemoteAddr = v.peer
		r.Header.Set("X-Forwarded-Proto", v.proto)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != v.want {
			t.Fatal(v, w.Code)
		}
	}
	p, _ := NewProxyPolicy(o.TrustedProxy)
	r := httptest.NewRequest("GET", "https://sub.example.test", nil)
	r.RemoteAddr = "127.0.0.1:1000"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-For", "198.51.100.5, 192.0.2.7, 127.0.0.1")
	if peer, err := p.Peer(r); err != nil || peer != "192.0.2.7" {
		t.Fatal(peer, err)
	}
}
