package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestEnrichmentHTTPRefreshRetainsLastGood(t *testing.T) {
	fail := false
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "Artist" || r.URL.Query().Get("key") != "key" || r.Header.Get("Authorization") != "" {
			t.Error("identity/credential contract")
		}
		if fail {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(model.MusicEnrichment{Description: "A real fixture biography\nSecond paragraph", Source: "Fixture", ImageURL: "https://example.org/artist.jpg", Links: []model.MusicMetadataLink{{Label: "MusicBrainz", URL: "https://musicbrainz.org/artist/example"}}})
	}))
	defer agent.Close()
	var saved *model.MusicEnrichment
	store := repository.MockMusicEnrichmentRepo{
		GetMusicEnrichmentFunc:  func(context.Context, string, string, string) (*model.MusicEnrichment, error) { return saved, nil },
		SaveMusicEnrichmentFunc: func(_ context.Context, _, _, _ string, item model.MusicEnrichment) error { saved = &item; return nil },
	}
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	id := EnrichmentIdentity{Kind: "artist", Key: "key", Name: "Artist"}
	provider := NewHTTPEnrichmentProvider(agent.URL)
	result, err := LoadMusicEnrichment(context.Background(), store, provider, agent.URL, id, false, true, now)
	if err != nil || result.State != "unfetched" || saved != nil {
		t.Fatalf("read fetched unexpectedly: %+v %v", result, err)
	}
	result, err = LoadMusicEnrichment(context.Background(), store, provider, agent.URL, id, true, true, now)
	if err != nil || result.State != "ready" || saved == nil || result.FetchedAt == nil {
		t.Fatalf("refresh: %+v %v", result, err)
	}
	fail = true
	result, err = LoadMusicEnrichment(context.Background(), store, provider, agent.URL, id, true, true, now.Add(31*24*time.Hour))
	if err != nil || result.State != "error" || result.Description != saved.Description || !result.Stale {
		t.Fatalf("lost good data: %+v %v", result, err)
	}
}

func TestEnrichmentProviderValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantError  bool
	}{
		{"empty", "", 204, false},
		{"unsafe link", `{"source":"Fixture","links":[{"label":"bad","url":"javascript:alert(1)"}]}`, 200, true},
		{"unattributed", `{"description":"A biography"}`, 200, true},
		{"bad json", `{`, 200, true},
		{"redirect", "", 302, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			_, err := NewHTTPEnrichmentProvider(server.URL).Fetch(context.Background(), EnrichmentIdentity{})
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v wantError=%v", err, tc.wantError)
			}
		})
	}
}

func TestEnrichmentIdentityIsPermissionDerived(t *testing.T) {
	albums := []Album{{Key: "visible", Title: "Album", Songs: []model.Media{{Music: &model.MusicMetadata{Artist: "Artist"}}}}}
	if _, found := MusicEnrichmentIdentity(albums, "album", "hidden"); found {
		t.Fatal("hidden identity resolved")
	}
	if _, found := MusicEnrichmentIdentity(albums, "song", "visible"); found {
		t.Fatal("unsupported kind resolved")
	}
	if id, found := MusicEnrichmentIdentity(albums, "album", "visible"); !found || id.Name != "Album" {
		t.Fatal("visible identity absent")
	}
}
