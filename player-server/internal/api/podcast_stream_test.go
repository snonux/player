package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

func TestPodcastStreamRoutes(t *testing.T) {
	srv, store, sm, admin, cleanup := setupPodcastE2E(t)
	defer cleanup()
	publisher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("leaked user session")
		}
		if r.Header.Get("Range") == "bytes=99999-" {
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Length", "13")
			w.Header().Set("Content-Range", "bytes */6")
			w.WriteHeader(416)
			_, _ = io.WriteString(w, "<b>error!</b>\n")
			return
		}
		if r.URL.Path == "/slow" {
			w.Header().Set("Content-Type", "audio/wav")
			for i := 0; i < 8; i++ {
				_, _ = io.WriteString(w, "x")
				w.(http.Flusher).Flush()
				time.Sleep(40 * time.Millisecond)
			}
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Set-Cookie", "publisher=secret")
		if r.Header.Get("Range") == "bytes=2-4" {
			w.Header().Set("Content-Range", "bytes 2-4/6")
			w.WriteHeader(206)
			_, _ = io.WriteString(w, "dio")
			return
		}
		_, _ = io.WriteString(w, "audio!")
	}))
	defer publisher.Close()
	ctx := context.Background()
	set, err := store.CreateSet(ctx, &model.Set{Name: "podcast-stream", RootPath: "podcast-stream", IsPodcast: true, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	feed, err := store.CreateFeed(ctx, &model.PodcastFeed{SetID: set, FeedURL: publisher.URL + "/rss", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	episode, err := store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feed, GUID: "stream", Title: "stream", EpisodeURL: publisher.URL + "/audio.wav", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	cookie := addSessionCookie(t, store, sm, admin)
	for _, prefix := range []string{"/api", "/api/v1"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			req := httptest.NewRequest(method, fmt.Sprintf("%s/podcasts/episodes/%d/stream", prefix, episode), nil)
			req.AddCookie(cookie)
			req.Header.Set("Range", "bytes=2-4")
			result := httptest.NewRecorder()
			srv.ServeHTTP(result, req)
			if result.Code != 206 || result.Header().Get("Content-Range") != "bytes 2-4/6" || result.Header().Get("Set-Cookie") != "" || result.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("invalid proxy response: %d %v", result.Code, result.Header())
			}
			if method == http.MethodHead && result.Body.Len() != 0 {
				t.Fatal("HEAD returned body")
			}
			if method == http.MethodGet && result.Body.String() != "dio" {
				t.Fatalf("wrong range: %q", result.Body.String())
			}
		}
		outOfRange := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s/podcasts/episodes/%d/stream", prefix, episode), nil)
		outOfRange.AddCookie(cookie)
		outOfRange.Header.Set("Range", "bytes=99999-")
		invalid := httptest.NewRecorder()
		srv.ServeHTTP(invalid, outOfRange)
		if invalid.Code != 416 || invalid.Body.Len() != 0 || invalid.Header().Get("Content-Length") != "0" || invalid.Header().Get("Content-Type") != "application/octet-stream" {
			t.Fatalf("unsafe or truncated range error: %d %v %q", invalid.Code, invalid.Header(), invalid.Body.String())
		}
		for _, id := range []string{"0", "bad", "999999"} {
			req := httptest.NewRequest(http.MethodGet, prefix+"/podcasts/episodes/"+id+"/stream", nil)
			req.AddCookie(cookie)
			result := httptest.NewRecorder()
			srv.ServeHTTP(result, req)
			want := 400
			if id == "999999" {
				want = 404
			}
			if result.Code != want {
				t.Fatalf("id%s: %d", id, result.Code)
			}
		}
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s/podcasts/episodes/%d/stream", prefix, episode), nil)
		result := httptest.NewRecorder()
		srv.ServeHTTP(result, req)
		if result.Code != 401 {
			t.Fatalf("anonymous stream: %d", result.Code)
		}
	}
	t.Run("stream survives server absolute write timeout", func(t *testing.T) {
		slow, err := store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feed, GUID: "slow", Title: "slow", EpisodeURL: publisher.URL + "/slow", CreatedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewUnstartedServer(srv)
		server.Config.WriteTimeout = 100 * time.Millisecond
		server.Start()
		defer server.Close()
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		request, _ := http.NewRequestWithContext(bounded, http.MethodGet, fmt.Sprintf("%s/api/podcasts/episodes/%d/stream", server.URL, slow), nil)
		request.AddCookie(cookie)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || string(body) != strings.Repeat("x", 8) {
			t.Fatalf("long stream cut by absolute deadline: %q %v", body, err)
		}
	})

	t.Run("download success survives server absolute write timeout", func(t *testing.T) {
		slow, err := store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feed, GUID: "slow-save", Title: "slow-save", EpisodeURL: publisher.URL + "/slow", CreatedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewUnstartedServer(srv)
		server.Config.WriteTimeout = 100 * time.Millisecond
		server.Start()
		defer server.Close()
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		request, _ := http.NewRequestWithContext(bounded, http.MethodPost, fmt.Sprintf("%s/api/podcasts/episodes/%d/download", server.URL, slow), nil)
		request.AddCookie(cookie)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var saved model.Media
		if err := json.NewDecoder(response.Body).Decode(&saved); err != nil || response.StatusCode != http.StatusOK || saved.ID <= 0 {
			t.Fatalf("slow save did not report its completed copy: status=%d media=%+v err=%v", response.StatusCode, saved, err)
		}
		row, err := store.GetEpisodeByID(ctx, slow)
		if err != nil || row == nil || row.MediaID == nil || *row.MediaID != saved.ID {
			t.Fatalf("completion response did not match persisted copy: %+v %v", row, err)
		}
	})

}
