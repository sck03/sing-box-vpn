package manage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestProbeHTTPStartupAndBoundedFailure(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		recover  bool
		requests int32
	}{
		{"starting", http.StatusBadGateway, true, 2},
		{"unavailable", http.StatusServiceUnavailable, false, 3},
		{"incorrect-path", http.StatusNotFound, false, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				status := test.status
				if requests.Add(1) > 1 && test.recover {
					status = http.StatusNoContent
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			err := probeHTTP(ctx, server.Client(), server.URL, nil)
			if (err == nil) != test.recover || requests.Load() != test.requests {
				t.Fatalf("requests=%d error=%v", requests.Load(), err)
			}
		})
	}
}
