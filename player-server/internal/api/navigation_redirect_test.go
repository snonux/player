package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestLibrarySignInReturn(t *testing.T) {
	for _, path := range []string{"/?set=6&folder=Beyonc%C3%A9&media=123", "/index.html?media=123", "/", "/stream/private?token=secret"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		NewMiddleware(nil, nil, nil).RequireSession(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unauthenticated request passed") })).ServeHTTP(w, r)
		if w.Code != http.StatusTemporaryRedirect {
			t.Fatalf("%s: status %d", path, w.Code)
		}
		location, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		if location.Path != "/login.html" {
			t.Fatalf("unexpected destination: %s", location)
		}
		want := ""
		if r.URL.RawQuery != "" && (r.URL.Path == "/" || r.URL.Path == "/index.html") {
			want = path
		}
		if got := location.Query().Get("return_to"); got != want {
			t.Fatalf("%s: return %q, want %q", path, got, want)
		}
	}
}
