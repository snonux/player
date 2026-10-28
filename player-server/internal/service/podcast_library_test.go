package service

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/podcast"
	"github.com/snonux/player/internal/repository"
)

// podcastLibraryFixture uses both SQLite schema shapes: a fresh database and
// an old single-root database rebuilt with NOT NULL library_id on upgrade.
func podcastLibraryFixture(t *testing.T, migrated bool) (*podcastService, *repository.SQLite, int64, int64) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "player.db")
	if migrated {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`CREATE TABLE sets (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, root_path TEXT NOT NULL UNIQUE, cover_thumbnail_path TEXT, is_podcast INTEGER NOT NULL DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
INSERT INTO sets (id, name, root_path) VALUES (9, 'Podcasts', 'Podcasts')`)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	store, err := repository.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if migrated {
		if _, err := store.CreateSet(ctx, &model.Set{Name: "invalid", RootPath: "invalid", CreatedAt: time.Now()}); err == nil {
			t.Fatal("migration fixture does not enforce library_id")
		}
	}
	root := t.TempDir()
	now := time.Now().UTC()
	lib, err := store.EnsureDefaultMusicLibrary(ctx, root, now)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateMusicLibrary(ctx, &model.MusicLibrary{Name: "AAA other", RootPath: t.TempDir(), CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSet(ctx, &model.Set{LibraryID: other, Name: "podcast", RootPath: "podcast", IsPodcast: true, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	admin, err := store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "h", IsAdmin: true, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(ctx, &model.User{Username: "listener", PasswordHash: "h", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewPodcastService(store, &clock.MockClock{T: now}, root, &accessHelper{store: store}, nil, nil, &http.Client{}, 60)
	svc.parseFeed = func(*http.Client, string) (*podcast.ParsedFeed, error) {
		return &podcast.ParsedFeed{Title: "Show", Episodes: []podcast.Episode{{GUID: "one", Title: "One", EpisodeURL: "https://example.com/one.mp3"}}}, nil
	}
	svc.downloadCover = func(*http.Client, string, string) error { return nil }
	if lib.ID == other {
		t.Fatal("fixture libraries coincide")
	}
	return svc, store, admin, user
}

func TestPodcastLibraryFirstSubscriptionSQLite(t *testing.T) {
	for _, migrated := range []bool{false, true} {
		for _, viaSync := range []bool{false, true} {
			name := "fresh"
			if migrated {
				name = "migrated"
			}
			if viaSync {
				name += "/sync"
			} else {
				name += "/admin"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				svc, store, admin, user := podcastLibraryFixture(t, migrated)
				const url = "https://example.com/rss"
				if viaSync {
					syncSvc := NewPodcastSyncService(store, svc.clock, 60, svc.mediaRoot)
					req := model.PodcastSyncRequest{Subscriptions: []model.PodcastSubscriptionEdit{{EditID: "first-subscribe-1", FeedKey: "feed:example.com/rss", FetchURL: url, Subscribed: true, ObservedMS: svc.clock.Now().UnixMilli()}}}
					out, err := syncSvc.Sync(ctx, user, req)
					if err != nil || len(out.Acked) != 1 {
						t.Fatalf("first sync: %+v %v", out, err)
					}
					if _, err := os.Stat(filepath.Join(svc.mediaRoot, "podcast")); !os.IsNotExist(err) {
						t.Fatalf("sync created media directory: %v", err)
					}
					// Replaying a first subscription shares the same catalog row.
					if _, err := syncSvc.Sync(ctx, user, req); err != nil {
						t.Fatal(err)
					}
				}
				first, err := svc.SubscribeFeed(ctx, url, "ignored", admin)
				if err != nil {
					t.Fatal(err)
				}
				second, err := svc.SubscribeFeed(ctx, "http://example.com/rss", "ignored", admin)
				if err != nil || second.ID != first.ID {
					t.Fatalf("dedup: %+v %v", second, err)
				}
				set, err := store.GetSetByID(ctx, first.SetID)
				if err != nil || set == nil || !set.IsPodcast || set.RootPath != "podcast" || set.LibraryID == 0 {
					t.Fatalf("managed set: %+v %v", set, err)
				}
				lib, err := store.GetMusicLibrary(ctx, set.LibraryID)
				if err != nil || lib.RootPath != svc.mediaRoot {
					t.Fatalf("wrong library: %+v %v", lib, err)
				}
				feeds, err := store.ListFeeds(ctx)
				if err != nil || len(feeds) != 1 {
					t.Fatalf("catalog: %+v %v", feeds, err)
				}
				eps, err := store.ListEpisodesByFeed(ctx, first.ID, 10, 0)
				if err != nil || len(eps) != 1 {
					t.Fatalf("episodes: %+v %v", eps, err)
				}
				subs, err := store.PodcastSubscriptionStates(ctx, admin)
				if err != nil || !subs["feed:example.com/rss"] {
					t.Fatalf("admin subscription: %+v %v", subs, err)
				}
				if migrated {
					ordinary, err := store.GetSetByID(ctx, 9)
					if err != nil || ordinary == nil || ordinary.IsPodcast || ordinary.Name != "Podcasts" || ordinary.RootPath != "Podcasts" {
						t.Fatalf("ordinary set 9 changed: %+v %v", ordinary, err)
					}
				}
			})
		}
	}
}

type failingPodcastSetStore struct {
	repository.Store
	createErr, grantErr error
}

func (s failingPodcastSetStore) CreateSet(ctx context.Context, set *model.Set) (int64, error) {
	if s.createErr != nil {
		return 0, s.createErr
	}
	return s.Store.CreateSet(ctx, set)
}
func (s failingPodcastSetStore) GrantPermission(ctx context.Context, p *model.SetPermission) error {
	if s.grantErr != nil {
		return s.grantErr
	}
	return s.Store.GrantPermission(ctx, p)
}

func TestPodcastLibraryProvisionFailurePreservesFiles(t *testing.T) {
	for _, failure := range []string{"create", "permission"} {
		for _, existing := range []bool{false, true} {
			t.Run(failure+map[bool]string{false: "/new", true: "/existing"}[existing], func(t *testing.T) {
				svc, store, admin, _ := podcastLibraryFixture(t, true)
				path := filepath.Join(svc.mediaRoot, "podcast")
				if existing {
					if err := os.Mkdir(path, 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(path, "keep.mp3"), []byte("precious"), 0644); err != nil {
						t.Fatal(err)
					}
				}
				boom := errors.New("injected store failure")
				wrapped := failingPodcastSetStore{Store: store}
				if failure == "create" {
					wrapped.createErr = boom
				} else {
					wrapped.grantErr = boom
				}
				svc.store = wrapped
				if _, err := svc.SubscribeFeed(context.Background(), "https://example.com/rss", "", admin); !errors.Is(err, boom) {
					t.Fatalf("error: %v", err)
				}
				if existing {
					data, err := os.ReadFile(filepath.Join(path, "keep.mp3"))
					if err != nil || string(data) != "precious" {
						t.Fatalf("existing media lost: %q %v", data, err)
					}
				} else if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("new directory not cleaned: %v", err)
				}
				feeds, err := store.ListFeeds(context.Background())
				if err != nil || len(feeds) != 0 {
					t.Fatalf("partial feed: %+v %v", feeds, err)
				}
				sets, err := store.ListSets(context.Background())
				if err != nil || len(sets) != 2 {
					t.Fatalf("partial set: %+v %v", sets, err)
				}
			})
		}
	}
}

func TestPodcastLibraryMissingRegistrationAndUnsafePaths(t *testing.T) {
	for _, mode := range []string{"unregistered", "missing-root", "read-only", "regular-file", "escaping-symlink", "ordinary-path"} {
		t.Run(mode, func(t *testing.T) {
			svc, store, admin, user := podcastLibraryFixture(t, true)
			ctx := context.Background()
			switch mode {
			case "unregistered":
				svc.mediaRoot = t.TempDir()
			case "missing-root":
				if err := os.Remove(svc.mediaRoot); err != nil {
					t.Fatal(err)
				}
			case "read-only":
				if os.Geteuid() == 0 {
					t.Skip("root bypasses permission bits")
				}
				if err := os.Chmod(svc.mediaRoot, 0555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(svc.mediaRoot, 0755) })
			case "regular-file":
				if err := os.WriteFile(filepath.Join(svc.mediaRoot, "podcast"), []byte("preserve"), 0644); err != nil {
					t.Fatal(err)
				}
			case "escaping-symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(svc.mediaRoot, "podcast")); err != nil {
					t.Fatal(err)
				}
			case "ordinary-path":
				libs, err := store.ListMusicLibraries(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for _, lib := range libs {
					if lib.RootPath == svc.mediaRoot {
						if _, err := store.CreateSet(ctx, &model.Set{LibraryID: lib.ID, Name: "Leave alone", RootPath: "podcast", CreatedAt: svc.clock.Now()}); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if _, err := svc.SubscribeFeed(ctx, "https://example.com/rss", "", admin); err == nil {
				t.Fatal("unsafe admin provisioning succeeded")
			} else if mode == "read-only" && !errors.Is(err, ErrReadOnlySet) {
				t.Fatalf("read-only error: %v", err)
			}
			feeds, err := store.ListFeeds(ctx)
			if err != nil || len(feeds) != 0 {
				t.Fatalf("failed admin created catalog: %+v %v", feeds, err)
			}
			// Sync performs no filesystem writes, even when media is missing or
			// read-only. Missing library registration and ordinary-set collisions
			// abort its entire transaction, including subscription register writes.
			_, err = NewPodcastSyncService(store, svc.clock, 60, svc.mediaRoot).Sync(ctx, user, model.PodcastSyncRequest{Subscriptions: []model.PodcastSubscriptionEdit{{EditID: "sync-first-sub-01", FeedKey: "feed:example.com/rss", Subscribed: true, ObservedMS: 1000}}})
			wantFailure := mode == "unregistered" || mode == "ordinary-path"
			if (err != nil) != wantFailure {
				t.Fatalf("sync error=%v, wantFailure=%v", err, wantFailure)
			}
			states, stateErr := store.PodcastSubscriptionStates(ctx, user)
			if stateErr != nil || states["feed:example.com/rss"] == wantFailure {
				t.Fatalf("sync register: %+v %v", states, stateErr)
			}
			sets, setErr := store.ListSets(ctx)
			if setErr != nil {
				t.Fatal(setErr)
			}
			for _, set := range sets {
				if set.Name == "Leave alone" && set.IsPodcast {
					t.Fatal("ordinary set converted to podcast")
				}
			}
		})
	}
}

func TestPodcastLibraryRepeatSubscriptionReadOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	ctx := context.Background()
	svc, store, admin, _ := podcastLibraryFixture(t, true)
	const url = "https://example.com/rss"
	feed, err := svc.SubscribeFeed(ctx, url, "", admin)
	if err != nil {
		t.Fatal(err)
	}
	setDir := filepath.Join(svc.mediaRoot, "podcast")
	showDir := filepath.Join(setDir, "Show")
	keepPath := filepath.Join(showDir, "keep.mp3")
	if err := os.WriteFile(keepPath, []byte("existing episode"), 0444); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{showDir, setDir, svc.mediaRoot} {
		if err := os.Chmod(dir, 0555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
	}
	// Verify real filesystem permissions are in force for the non-root test
	// process; the retry must need only database metadata writes.
	if f, err := os.CreateTemp(setDir, ".unexpected-write-"); err == nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		t.Fatal("read-only directory unexpectedly writable")
	}
	svc.parseFeed = func(*http.Client, string) (*podcast.ParsedFeed, error) {
		return &podcast.ParsedFeed{Title: "Show", Description: "Refreshed metadata"}, nil
	}
	svc.downloadCover = podcast.DownloadCoverImage
	repeated, err := svc.SubscribeFeed(ctx, url, "", admin)
	if err != nil {
		t.Fatalf("repeat subscription on existing read-only directories: %v", err)
	}
	if repeated.ID != feed.ID {
		t.Fatalf("repeat created another feed: %d != %d", repeated.ID, feed.ID)
	}
	persisted, err := store.GetFeedByID(ctx, feed.ID)
	if err != nil || persisted.Description != "Refreshed metadata" {
		t.Fatalf("metadata refresh: %+v %v", persisted, err)
	}
	data, err := os.ReadFile(keepPath)
	if err != nil || string(data) != "existing episode" {
		t.Fatalf("existing episode: %q %v", data, err)
	}
	// A changed title requiring a new feed folder still needs a real write.
	svc.parseFeed = func(*http.Client, string) (*podcast.ParsedFeed, error) {
		return &podcast.ParsedFeed{Title: "Another show"}, nil
	}
	if _, err := svc.SubscribeFeed(ctx, url, "", admin); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("new folder on read-only media: %v", err)
	}
	if _, err := os.Stat(filepath.Join(setDir, "Another show")); !os.IsNotExist(err) {
		t.Fatalf("new folder was created: %v", err)
	}
}
