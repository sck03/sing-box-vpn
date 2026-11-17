package manage

import (
	"net/http"
	"strings"
)

// Attach credentials at the transport boundary to a cloned request, so the
// client's redirect machinery never copies them to a different destination.
type githubMetadataTransport struct {
	base  http.RoundTripper
	token string
}

func (t githubMetadataTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if t.token != "" && request.URL.Scheme == "https" && request.URL.Host == "api.github.com" && request.URL.User == nil && officialReleaseMetadata(request.URL.Path) {
		request = request.Clone(request.Context())
		request.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(request)
}

func officialReleaseMetadata(path string) bool {
	for _, repo := range []string{"SagerNet/sing-box", "cloudflare/cloudflared", "sck03/sing-box-vpn"} {
		prefix := "/repos/" + repo + "/releases/"
		if path == prefix+"latest" || strings.HasPrefix(path, prefix+"tags/") {
			return true
		}
	}
	return false
}
