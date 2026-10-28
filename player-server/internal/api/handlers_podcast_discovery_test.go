package api

import (
	"github.com/snonux/player/internal/service"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPodcastDiscoveryPublicAliases(t *testing.T) {
	s := &Server{mux: http.NewServeMux(), mw: &Middleware{}, podcastDiscovery: service.NewPodcastDiscovery()}
	s.routesPublic()
	for _, prefix := range []string{"/api/", "/api/v1/"} {
		recorder := httptest.NewRecorder()
		s.mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, prefix+"podcast-discovery?q=a", nil))
		if recorder.Code != http.StatusBadRequest || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s status %d", prefix, recorder.Code)
		}
		recorder = httptest.NewRecorder()
		s.mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, prefix+"podcast-discovery?q=a", nil))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s POST status %d", prefix, recorder.Code)
		}
	}
}
