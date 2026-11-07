package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

func TestPodcastStreamRangeAndCredentials(t *testing.T) {
	f := newUnsubscribeFixture(t)
	publisher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "" {
			t.Error("publisher received credentials")
		}
		if r.Header.Get("Range") != "bytes=2-4" || r.Header.Get("If-Range") != "version" {
			t.Error("range lost")
		}
		if r.URL.Path == "/audio" {
			w.Header().Set("Set-Cookie", "publisher=secret")
			http.Redirect(w, r, "/redirected", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Range", "bytes 2-4/6")
		w.WriteHeader(http.StatusPartialContent)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, "dio")
		}
	}))
	defer publisher.Close()
	episode, err := f.store.CreateEpisode(context.Background(), &model.PodcastEpisode{FeedID: f.other, GUID: "range", Title: "range", EpisodeURL: publisher.URL + "/audio", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response, err := f.svc.OpenEpisodeStream(context.Background(), episode, f.viewer, method, "bytes=2-4", "version")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusPartialContent || response.Header.Get("Content-Range") != "bytes 2-4/6" {
			t.Fatalf("range response: %v %v", response, err)
		}
		if method == http.MethodGet && string(body) != "dio" {
			t.Fatalf("body %q", body)
		}
	}
}

func TestPodcastStreamRejectsAccessAndPublisherFailures(t *testing.T) {
	for _, scenario := range []string{"forbidden", "missing", "html", "failure", "restricted", "userinfo", "redirect-userinfo"} {
		t.Run(scenario, func(t *testing.T) {
			f := newUnsubscribeFixture(t)
			calls := 0
			publisher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if scenario == "redirect-userinfo" {
					http.Redirect(w, r, "http://secret:password@"+r.Host+"/redirected", http.StatusFound)
					return
				}
				switch scenario {
				case "html":
					w.Header().Set("Content-Type", "text/html")
				case "failure":
					w.WriteHeader(500)
				default:
					w.Header().Set("Content-Type", "audio/mpeg")
				}
				_, _ = io.WriteString(w, "audio")
			}))
			defer publisher.Close()
			target := publisher.URL + "/audio"
			if scenario == "userinfo" {
				target = strings.Replace(target, "http://", "http://secret:password@", 1)
			}
			episode, err := f.store.CreateEpisode(context.Background(), &model.PodcastEpisode{FeedID: f.other, GUID: "reject", Title: "reject", EpisodeURL: target, CreatedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			user := f.viewer
			if scenario == "forbidden" {
				user = 999999
			}
			if scenario == "missing" {
				episode = 999999
			}
			if scenario == "restricted" {
				original, _ := f.store.GetFeedByID(context.Background(), f.other)
				feedID, err := f.store.CreateFeed(context.Background(), &model.PodcastFeed{SetID: original.SetID, FeedURL: publisher.URL + "/restricted-rss", CreatedBy: &f.viewer, CreatedAt: time.Now()})
				if err != nil {
					t.Fatal(err)
				}
				episode, err = f.store.CreateEpisode(context.Background(), &model.PodcastEpisode{FeedID: feedID, GUID: "restricted", Title: "restricted", EpisodeURL: target, CreatedAt: time.Now()})
				if err != nil {
					t.Fatal(err)
				}
			}
			response, err := f.svc.OpenEpisodeStream(context.Background(), episode, user, http.MethodGet, "", "")
			if response != nil {
				response.Body.Close()
				t.Fatal("accepted invalid stream")
			}
			if err == nil {
				t.Fatal("missing error")
			}
			if (scenario == "forbidden" || scenario == "missing" || scenario == "userinfo" || scenario == "restricted") && calls != 0 {
				t.Fatal("contacted forbidden publisher")
			}
		})
	}
}

func TestPodcastStreamSubscriberWithoutServerCopyPermission(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx := context.Background()
	publisher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = io.WriteString(w, "publisher audio")
	}))
	defer publisher.Close()
	user, err := f.store.CreateUser(ctx, &model.User{Username: "stream-only", PasswordHash: "h", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	feed, err := f.store.GetFeedByID(ctx, f.other)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetPodcastSubscription(ctx, user, feed, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	episode, err := f.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: f.other, GUID: "subscriber", Title: "subscriber", EpisodeURL: publisher.URL, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	response, err := f.svc.OpenEpisodeStream(ctx, episode, user, http.MethodGet, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "publisher audio" {
		t.Fatalf("subscribed publisher inaccessible: %q %v", body, err)
	}
	if err := f.svc.helper.checkSetPermission(ctx, feed.SetID, user, ""); !errors.Is(err, ErrForbidden) {
		t.Fatal("streaming granted server-copy permission")
	}
}

func TestPodcastStreamCancellationDoesNotCancelOtherStream(t *testing.T) {
	f := newUnsubscribeFixture(t)
	publisher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = io.WriteString(w, "initial")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer publisher.Close()
	episode, err := f.store.CreateEpisode(context.Background(), &model.PodcastEpisode{FeedID: f.other, GUID: "cancel-stream", Title: "cancel", EpisodeURL: publisher.URL, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	first, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	second, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	a, err := f.svc.OpenEpisodeStream(first, episode, f.viewer, http.MethodGet, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Body.Close()
	b, err := f.svc.OpenEpisodeStream(second, episode, f.viewer, http.MethodGet, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Body.Close()
	cancelFirst()
	_, err = io.ReadAll(a.Body)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("first not cancelled: %v", err)
	}
	data := make([]byte, 7)
	if _, err := io.ReadFull(b.Body, data); err != nil || string(data) != "initial" {
		t.Fatalf("second stream interrupted: %q %v", data, err)
	}
	cancelSecond()
}
