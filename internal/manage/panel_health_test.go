package manage

import (
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type heldPanelResponse struct {
	*httptest.ResponseRecorder
	started chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (w *heldPanelResponse) Write(data []byte) (int, error) {
	w.once.Do(func() { w.started <- struct{}{} })
	<-w.release
	return w.ResponseRecorder.Write(data)
}

func TestPanelHealthSurvivesSaturatedRequests(t *testing.T) {
	s := &PlatformService{}
	s.workerReady.Store(true)
	handler := s.Handler(PanelOptions{Listen: "127.0.0.1:2096", PublicURL: "http://example.com"})
	started, release := make(chan struct{}, 32), make(chan struct{})
	var clients sync.WaitGroup
	defer clients.Wait()
	defer close(release)
	for range 32 {
		clients.Go(func() {
			writer := &heldPanelResponse{ResponseRecorder: httptest.NewRecorder(), started: started, release: release}
			handler.ServeHTTP(writer, httptest.NewRequest("GET", "http://example.com/", nil))
		})
	}
	for range 32 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("application requests did not reach the concurrency limit")
		}
	}
	request := httptest.NewRequest("GET", "http://example.com/healthz", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != 200 {
		t.Fatalf("ready panel reported unavailable under load: %d %s", w.Code, w.Body.String())
	}
	s.workerReady.Store(false)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != 503 {
		t.Fatal("health check accepted a worker that is not ready")
	}
}
