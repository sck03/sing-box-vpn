package manage

import (
	"bytes"
	"net/http"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// ServeContent alone decides conditional/range semantics. Reserve allowance
// immediately before a successful body response, including partial content.
func serveSubscriptionArtifact(w http.ResponseWriter, r *http.Request, artifact sub.Artifact, reserve func() error) {
	writer := &subscriptionResponse{ResponseWriter: w, request: r, reserve: reserve}
	http.ServeContent(writer, r, artifact.Name, time.Time{}, bytes.NewReader(artifact.Data))
}

type subscriptionResponse struct {
	http.ResponseWriter
	request *http.Request
	reserve func() error
	wrote   bool
	denied  bool
}

func (w *subscriptionResponse) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	if w.request.Method == http.MethodGet && (status == http.StatusOK || status == http.StatusPartialContent) {
		if err := w.reserve(); err != nil {
			w.denied = true
			for _, key := range []string{"Content-Length", "Content-Range", "ETag", "subscription-userinfo", "Accept-Ranges"} {
				w.Header().Del(key)
			}
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w.ResponseWriter, w.request)
			return
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *subscriptionResponse) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if w.denied {
		return len(data), nil
	}
	return w.ResponseWriter.Write(data)
}
