package manage

import (
	"net/http"
	"testing"
)

func TestGitHubMetadataCredentialsDoNotFollowRedirects(t *testing.T) {
	t.Setenv("SBVPN_GITHUB_TOKEN", "test-only-token")
	for _, destination := range []string{
		"https://downloads.example.test/archive",
		"https://untrusted.api.github.com/archive",
		"https://api.github.com/repos/other/project/releases/latest",
		"http://api.github.com/repos/SagerNet/sing-box/releases/latest",
		"https://api.github.com:8443/repos/SagerNet/sing-box/releases/latest",
	} {
		t.Run(destination, func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
				requests++
				response := byteResponse([]byte(`{}`))
				if requests == 1 {
					if r.Header.Get("Authorization") != "Bearer test-only-token" {
						t.Fatal("official metadata request is unauthenticated")
					}
					response.StatusCode = http.StatusFound
					response.Header.Set("Location", destination)
				} else if r.Header.Get("Authorization") != "" {
					t.Fatal("metadata token escaped to redirect destination")
				}
				return response, nil
			})}
			var data Object
			if err := getJSON(t.Context(), client, "https://api.github.com/repos/SagerNet/sing-box/releases/latest", &data); err != nil {
				t.Fatal(err)
			}
			if requests != 2 {
				t.Fatal("redirect not exercised")
			}
		})
	}
}
