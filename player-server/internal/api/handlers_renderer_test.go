package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRendererTime(t *testing.T) {
	for _, seconds := range []float64{0, 62, 3599, 3601, 86400} {
		if got := rendererSeconds(rendererTime(seconds)); got != seconds {
			t.Fatalf("%v roundtrip %v", seconds, got)
		}
	}
	for _, value := range []string{"NOT_IMPLEMENTED", "", "0", "-1:00:00"} {
		if rendererSeconds(value) != 0 {
			t.Fatal(value)
		}
	}
}

func TestRendererAPIAuthenticationAndInput(t *testing.T) {
	env := newCastTestEnv(t)
	handler := env.srv
	for _, path := range []string{"/api/renderers/control", "/api/v1/renderers/control"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"session_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","revision":1,"device_id":"arbitrary","action":"status"}`))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s unauthenticated: %d", path, response.Code)
		}
		for _, body := range []string{`{"session_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","revision":1,"device_id":"arbitrary","action":"fetch","url":"http://127.0.0.1"}`, `{"session_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","revision":1,"device_id":"arbitrary","action":"seek","position":-1}`, `{"session_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","revision":1,"device_id":"arbitrary","action":"load","media_id":99}`, `{"session_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","revision":1,"device_id":"arbitrary","action":"status"}`} {
			req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.AddCookie(env.cookie)
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code == http.StatusNotFound && strings.Contains(body, "status") {
				t.Fatalf("renderer control route missing: %s", path)
			}
			if response.Code == http.StatusOK {
				t.Fatalf("accepted invalid/undiscovered command %s", body)
			}
		}
	}
}
