package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/repository"
)

func TestServerIdentityPublicAliases(t *testing.T) {
	store, err := repository.Open(filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	srv := newTestServer(t, store, nil, nil, &internal.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	var previous string
	for _, path := range []string{"/api/server-identity", "/api/v1/server-identity"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			srv.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("identity must not be cached")
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			id := body["server_id"]
			if len(id) != 64 {
				t.Fatalf("invalid ID %q", id)
			}
			if previous != "" && previous != id {
				t.Fatal("aliases returned different IDs")
			}
			previous = id
		})
	}
}
