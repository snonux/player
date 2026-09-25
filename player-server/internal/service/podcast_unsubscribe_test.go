package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// unsubscribeFixture is a real SQLite store with one podcast set holding
// three feeds: "Show" twice (sharing the folder "Show") and "Other".
type unsubscribeFixture struct {
	svc                      *podcastService
	store                    *repository.SQLite
	root                     string
	admin, viewer            int64
	showA, showB, other      int64
	showAMedia, showBMedia   int64
	showAFile, showBFile     string
	otherMedia               int64
	otherFile, otherCoverImg string
}

func newUnsubscribeFixture(t *testing.T) *unsubscribeFixture {
	t.Helper()
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := t.TempDir()
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	f := &unsubscribeFixture{store: store, root: root}
	must := func(id int64, err error) int64 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.admin = must(store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "h", IsAdmin: true, CreatedAt: now}))
	f.viewer = must(store.CreateUser(ctx, &model.User{Username: "viewer", PasswordHash: "h", CreatedAt: now}))
	set := must(store.CreateSet(ctx, &model.Set{Name: "podcast", RootPath: "podcast", IsPodcast: true, CreatedAt: now}))
	if err := store.GrantPermission(ctx, &model.SetPermission{SetID: set, UserID: f.viewer, Role: model.RoleViewer, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	// feed creates a feed with one downloaded episode whose file exists on disk.
	feed := func(title, file string) (feedID, mediaID int64, path string) {
		feedID = must(store.CreateFeed(ctx, &model.PodcastFeed{SetID: set, FeedURL: "http://x/" + file, Title: title, CheckIntervalMinutes: 60, CreatedAt: now}))
		path = filepath.Join(root, "podcast", title, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
			t.Fatal(err)
		}
		rel := filepath.Join(title, file)
		mediaID = must(store.CreateMedia(ctx, &model.Media{SetID: set, RelPath: rel, FileName: file, AbsPath: path, Type: model.MediaTypeAudio, CreatedAt: now}))
		episode := must(store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feedID, GUID: file, Title: file, EpisodeURL: "http://x/" + file, CreatedAt: now}))
		if err := store.UpdateEpisodeMedia(ctx, episode, mediaID, file); err != nil {
			t.Fatal(err)
		}
		return feedID, mediaID, path
	}
	f.showA, f.showAMedia, f.showAFile = feed("Show", "a.mp3")
	f.showB, f.showBMedia, f.showBFile = feed("Show", "b.mp3")
	f.other, f.otherMedia, f.otherFile = feed("Other", "c.mp3")
	f.otherCoverImg = filepath.Join(root, "podcast", "Other", "cover.jpg")
	if err := os.WriteFile(f.otherCoverImg, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.svc = NewPodcastServiceWithLogger(store, &clock.MockClock{T: now}, root, NewAccessHelper(store), nil, nil, &http.Client{}, 60, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return f
}

func (f *unsubscribeFixture) mediaExists(t *testing.T, id int64) bool {
	t.Helper()
	m, err := f.store.GetMediaByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return m != nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestUnsubscribeFeed_SharedFolderKeepsOtherFeedsFiles(t *testing.T) {
	f := newUnsubscribeFixture(t)
	if err := f.svc.UnsubscribeFeed(context.Background(), f.showA, f.admin); err != nil {
		t.Fatal(err)
	}
	if feed, _ := f.store.GetFeedByID(context.Background(), f.showA); feed != nil {
		t.Fatal("feed still exists")
	}
	if f.mediaExists(t, f.showAMedia) || fileExists(f.showAFile) {
		t.Fatal("unsubscribed feed's episode media or file remains and would be re-imported")
	}
	if !f.mediaExists(t, f.showBMedia) || !fileExists(f.showBFile) {
		t.Fatal("the other feed sharing the folder lost its episode")
	}
}

func TestUnsubscribeFeed_RemovesOwnFolder(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx := context.Background()
	if err := f.svc.UnsubscribeFeed(ctx, f.other, f.admin); err != nil {
		t.Fatal(err)
	}
	if f.mediaExists(t, f.otherMedia) || fileExists(f.otherFile) || fileExists(f.otherCoverImg) {
		t.Fatal("feed episode, artwork or media row remains")
	}
	if fileExists(filepath.Dir(f.otherFile)) {
		t.Fatal("emptied feed folder was left behind")
	}
	if !f.mediaExists(t, f.showAMedia) || !fileExists(f.showAFile) {
		t.Fatal("unrelated feed was touched")
	}
}

// TestUnsubscribeFeed_KeepsUserFilesInFeedFolder: folder names come from
// titles, so a feed folder can hold a user's own uploads (or be a user
// folder that shares the title). Those are never deleted.
func TestUnsubscribeFeed_KeepsUserFilesInFeedFolder(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx := context.Background()
	upload := filepath.Join(f.root, "podcast", "Other", "my-upload.mp3")
	if err := os.WriteFile(upload, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	feed, _ := f.store.GetFeedByID(ctx, f.other)
	uploadID, err := f.store.CreateMedia(ctx, &model.Media{SetID: feed.SetID, RelPath: "Other/my-upload.mp3", FileName: "my-upload.mp3", AbsPath: upload, Type: model.MediaTypeAudio, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	// An unindexed file (e.g. not scanned yet) must survive as well.
	pending := filepath.Join(f.root, "podcast", "Other", "notes.txt")
	if err := os.WriteFile(pending, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.UnsubscribeFeed(ctx, f.other, f.admin); err != nil {
		t.Fatal(err)
	}
	if f.mediaExists(t, f.otherMedia) || fileExists(f.otherFile) {
		t.Fatal("the feed's own episode remains")
	}
	if !f.mediaExists(t, uploadID) || !fileExists(upload) || !fileExists(pending) {
		t.Fatal("a user's file in the feed folder was deleted")
	}
}

// TestUnsubscribeFeed_KeepsFolderWithOnlyUnindexedFiles: artwork goes, but
// the directory stays when files the library never indexed are inside.
func TestUnsubscribeFeed_KeepsFolderWithOnlyUnindexedFiles(t *testing.T) {
	f := newUnsubscribeFixture(t)
	pending := filepath.Join(f.root, "podcast", "Other", "notes.txt")
	if err := os.WriteFile(pending, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UnsubscribeFeed(context.Background(), f.other, f.admin); err != nil {
		t.Fatal(err)
	}
	if !fileExists(pending) {
		t.Fatal("unindexed file was deleted")
	}
	if fileExists(f.otherCoverImg) {
		t.Fatal("feed artwork should be removed")
	}
}

func TestUnsubscribeFeed_DeniedAndMissingChangeNothing(t *testing.T) {
	f := newUnsubscribeFixture(t)
	if err := f.svc.UnsubscribeFeed(context.Background(), f.other, f.viewer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer unsubscribe: got %v, want ErrForbidden", err)
	}
	if !f.mediaExists(t, f.otherMedia) || !fileExists(f.otherFile) {
		t.Fatal("denied unsubscribe deleted data")
	}
	if err := f.svc.UnsubscribeFeed(context.Background(), 9999, f.admin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing feed: got %v, want ErrNotFound", err)
	}
}

func TestInsideMediaRoot(t *testing.T) {
	f := newUnsubscribeFixture(t)
	sub := f.svc.podcastSubscriptionService
	for path, want := range map[string]bool{
		filepath.Join(f.root, "podcast", "x.mp3"): true,
		f.root:                                   false,
		filepath.Join(f.root, "..", "x.mp3"):     false,
		filepath.Join(f.root, "..evil", "x.mp3"): true,
		"/etc/passwd":                            false,
	} {
		if got := sub.insideMediaRoot(path); got != want {
			t.Errorf("insideMediaRoot(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestDownloadEpisode_FeedUnsubscribedMidDownload deletes the feed while the
// enclosure is being served. The download must report not found and leave no
// media row, file or folder behind for a rescan to re-import.
func TestDownloadEpisode_FeedUnsubscribedMidDownload(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx := context.Background()
	feedID := f.other
	var episodeID int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := f.svc.UnsubscribeFeed(ctx, feedID, f.admin); err != nil {
			t.Errorf("unsubscribe during download: %v", err)
		}
		_, _ = w.Write([]byte("late audio"))
	}))
	defer server.Close()
	id, err := f.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feedID, GUID: "late", Title: "late", EpisodeURL: server.URL + "/late.mp3", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	episodeID = id

	_, err = f.svc.DownloadEpisode(ctx, episodeID, f.admin)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	media, err := f.store.ListMedia(ctx, repository.MediaFilter{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range media {
		if strings.HasPrefix(m.RelPath, "Other/") {
			t.Fatalf("orphaned media row %q left after unsubscribe", m.RelPath)
		}
	}
	if fileExists(filepath.Join(f.root, "podcast", "Other")) {
		t.Fatal("feed folder recreated by the late download was left behind")
	}
}

// TestUnsubscribeFeed_RemovesEpisodesFromOldFolderName covers a feed whose
// title (and so folder name) changed after an episode was downloaded.
func TestUnsubscribeFeed_RemovesEpisodesFromOldFolderName(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx := context.Background()
	feed, _ := f.store.GetFeedByID(ctx, f.other)
	old := filepath.Join(f.root, "podcast", "Old Title", "early.mp3")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	mediaID, err := f.store.CreateMedia(ctx, &model.Media{SetID: feed.SetID, RelPath: "Old Title/early.mp3", FileName: "early.mp3", AbsPath: old, Type: model.MediaTypeAudio, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	episode, err := f.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: f.other, GUID: "early", Title: "early", EpisodeURL: "http://x/early.mp3", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateEpisodeMedia(ctx, episode, mediaID, "early.mp3"); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.UnsubscribeFeed(ctx, f.other, f.admin); err != nil {
		t.Fatal(err)
	}
	if f.mediaExists(t, mediaID) || fileExists(old) {
		t.Fatal("episode downloaded under the old folder name survived unsubscribe")
	}
	if fileExists(filepath.Dir(old)) {
		t.Fatal("old-title folder no feed uses any more was left behind")
	}
}

// TestUnsubscribeFeed_KeepsMediaOfRenamedFeedInSameFolder: feed "Other" was
// renamed from "Show"'s title earlier and still links an episode stored in
// Show/. Unsubscribing both "Show" feeds must not delete that episode.
func TestUnsubscribeFeed_KeepsMediaOfRenamedFeedInSameFolder(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx := context.Background()
	feed, _ := f.store.GetFeedByID(ctx, f.other)
	legacy := filepath.Join(f.root, "podcast", "Show", "legacy.mp3")
	if err := os.WriteFile(legacy, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	mediaID, err := f.store.CreateMedia(ctx, &model.Media{SetID: feed.SetID, RelPath: "Show/legacy.mp3", FileName: "legacy.mp3", AbsPath: legacy, Type: model.MediaTypeAudio, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	episode, err := f.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: f.other, GUID: "legacy", Title: "legacy", EpisodeURL: "http://x/legacy.mp3", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateEpisodeMedia(ctx, episode, mediaID, "legacy.mp3"); err != nil {
		t.Fatal(err)
	}

	// With showB gone, showA owns Show/ by title; the folder is then cleared.
	for _, id := range []int64{f.showB, f.showA} {
		if err := f.svc.UnsubscribeFeed(ctx, id, f.admin); err != nil {
			t.Fatal(err)
		}
	}
	if !f.mediaExists(t, mediaID) || !fileExists(legacy) {
		t.Fatal("another feed's episode in the shared folder name was deleted")
	}
	if fileExists(f.showAFile) || fileExists(f.showBFile) {
		t.Fatal("unsubscribed feeds' own episodes remain")
	}
}

// TestUnsubscribeFeed_KeptFolderKeepsItsCover: when another feed's episode
// keeps a folder alive, the folder is left untouched, cover included.
func TestUnsubscribeFeed_KeptFolderKeepsItsCover(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx := context.Background()
	feed, _ := f.store.GetFeedByID(ctx, f.other)
	cover := filepath.Join(f.root, "podcast", "Show", ".cover.jpg")
	if err := os.WriteFile(cover, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	coverID, err := f.store.CreateMedia(ctx, &model.Media{SetID: feed.SetID, RelPath: "Show/.cover.jpg", FileName: ".cover.jpg", AbsPath: cover, Type: model.MediaTypeImage, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	// "Other" links an episode stored in Show/, keeping that folder alive.
	legacy := filepath.Join(f.root, "podcast", "Show", "legacy.mp3")
	if err := os.WriteFile(legacy, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacyID, _ := f.store.CreateMedia(ctx, &model.Media{SetID: feed.SetID, RelPath: "Show/legacy.mp3", FileName: "legacy.mp3", AbsPath: legacy, Type: model.MediaTypeAudio, CreatedAt: time.Now()})
	ep, _ := f.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: f.other, GUID: "legacy", Title: "legacy", EpisodeURL: "http://x/l.mp3", CreatedAt: time.Now()})
	if err := f.store.UpdateEpisodeMedia(ctx, ep, legacyID, "legacy.mp3"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{f.showB, f.showA} {
		if err := f.svc.UnsubscribeFeed(ctx, id, f.admin); err != nil {
			t.Fatal(err)
		}
	}
	if !fileExists(cover) || !f.mediaExists(t, coverID) {
		t.Fatal("kept folder lost its cover")
	}
}

// TestUnsubscribeFeed_ContinuesAfterFileError: one undeletable file must not
// stop the rest; the error is still reported.
func TestUnsubscribeFeed_ContinuesAfterFileError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	f := newUnsubscribeFixture(t)
	ctx := context.Background()
	feed, _ := f.store.GetFeedByID(ctx, f.showA)
	lockedDir := filepath.Join(f.root, "podcast", "Locked")
	if err := os.MkdirAll(lockedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stuck := filepath.Join(lockedDir, "stuck.mp3")
	if err := os.WriteFile(stuck, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stuckID, _ := f.store.CreateMedia(ctx, &model.Media{SetID: feed.SetID, RelPath: "Locked/stuck.mp3", FileName: "stuck.mp3", AbsPath: stuck, Type: model.MediaTypeAudio, CreatedAt: time.Now()})
	ep, _ := f.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: f.showA, GUID: "stuck", Title: "stuck", EpisodeURL: "http://x/s.mp3", CreatedAt: time.Now()})
	if err := f.store.UpdateEpisodeMedia(ctx, ep, stuckID, "stuck.mp3"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockedDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o755) })

	// Episode media is processed in ListMedia order (by file name), so this
	// episode, sorting after "stuck.mp3", is only removed if processing
	// continues past the failure.
	later := f.addEpisode(t, f.showA, "Later", "zz-later.mp3")

	err := f.svc.UnsubscribeFeed(ctx, f.showA, f.admin)
	if err == nil {
		t.Fatal("expected the undeletable file to be reported")
	}
	if f.rowExists(t, later.id) || fileExists(later.path) {
		t.Fatal("an episode after the failing one was skipped")
	}
	if fileExists(filepath.Dir(later.path)) {
		t.Fatal("folder tidying did not run after the error")
	}
}

func TestTopFolder(t *testing.T) {
	for in, want := range map[string]string{
		"Show/a.mp3":     "Show",
		"Show/sub/a.mp3": "Show",
		"a.mp3":          "",
		"/a.mp3":         "",
		"../a.mp3":       "",
		"./a.mp3":        "",
	} {
		if got := topFolder(in); got != want {
			t.Errorf("topFolder(%q) = %q, want %q", in, got, want)
		}
	}
}

type fixtureEpisode struct {
	id   int64
	path string
}

// addEpisode creates a downloaded episode of feedID stored in folder/name.
func (f *unsubscribeFixture) addEpisode(t *testing.T, feedID int64, folder, name string) fixtureEpisode {
	t.Helper()
	ctx := context.Background()
	feed, _ := f.store.GetFeedByID(ctx, feedID)
	path := filepath.Join(f.root, "podcast", folder, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := f.store.CreateMedia(ctx, &model.Media{SetID: feed.SetID, RelPath: folder + "/" + name, FileName: name, AbsPath: path, Type: model.MediaTypeAudio, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	ep, err := f.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: feedID, GUID: folder + name, Title: name, EpisodeURL: "http://x/" + name, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateEpisodeMedia(ctx, ep, id, name); err != nil {
		t.Fatal(err)
	}
	return fixtureEpisode{id: id, path: path}
}

// rowExists reports whether a media row exists, trashed or not.
func (f *unsubscribeFixture) rowExists(t *testing.T, id int64) bool {
	t.Helper()
	all, err := f.store.ListMedia(context.Background(), repository.MediaFilter{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if m.ID == id {
			return true
		}
	}
	return false
}

// TestUnsubscribeFeed_RemovesTrashedEpisodes: a trashed episode must not
// survive (and keep its folder, or be restorable without a feed).
func TestUnsubscribeFeed_RemovesTrashedEpisodes(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx := context.Background()
	if err := f.store.SoftDeleteMedia(ctx, f.otherMedia); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UnsubscribeFeed(ctx, f.other, f.admin); err != nil {
		t.Fatal(err)
	}
	if f.rowExists(t, f.otherMedia) || fileExists(f.otherFile) {
		t.Fatal("trashed episode row or file survived")
	}
	if fileExists(filepath.Dir(f.otherFile)) {
		t.Fatal("folder kept alive by a trashed episode")
	}
}

// TestUnsubscribeFeed_KeepsFolderOfSameTitleFeed: another feed with the
// same title (no downloads yet) still owns the folder and its cover art.
func TestUnsubscribeFeed_KeepsFolderOfSameTitleFeed(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx := context.Background()
	feed, _ := f.store.GetFeedByID(ctx, f.other)
	twinA, _ := f.store.CreateFeed(ctx, &model.PodcastFeed{SetID: feed.SetID, FeedURL: "http://x/twin-a", Title: "Twin", CheckIntervalMinutes: 60, CreatedAt: time.Now()})
	if _, err := f.store.CreateFeed(ctx, &model.PodcastFeed{SetID: feed.SetID, FeedURL: "http://x/twin-b", Title: "Twin", CheckIntervalMinutes: 60, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	cover := filepath.Join(f.root, "podcast", "Twin", "cover.jpg")
	if err := os.MkdirAll(filepath.Dir(cover), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cover, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	coverID, _ := f.store.CreateMedia(ctx, &model.Media{SetID: feed.SetID, RelPath: "Twin/cover.jpg", FileName: "cover.jpg", AbsPath: cover, Type: model.MediaTypeImage, CreatedAt: time.Now()})

	if err := f.svc.UnsubscribeFeed(ctx, twinA, f.admin); err != nil {
		t.Fatal(err)
	}
	if !fileExists(cover) || !f.rowExists(t, coverID) {
		t.Fatal("the remaining same-title feed lost its cover")
	}
}

// TestUnsubscribeFeed_NeverDeletesFilesOutsideMediaRoot: a media row whose
// path points outside the media root loses its row but never its file.
func TestUnsubscribeFeed_NeverDeletesFilesOutsideMediaRoot(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx := context.Background()
	outside := filepath.Join(t.TempDir(), "elsewhere.mp3")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	feed, _ := f.store.GetFeedByID(ctx, f.other)
	id, _ := f.store.CreateMedia(ctx, &model.Media{SetID: feed.SetID, RelPath: "Other/elsewhere.mp3", FileName: "elsewhere.mp3", AbsPath: outside, Type: model.MediaTypeAudio, CreatedAt: time.Now()})
	ep, _ := f.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: f.other, GUID: "out", Title: "out", EpisodeURL: "http://x/out", CreatedAt: time.Now()})
	if err := f.store.UpdateEpisodeMedia(ctx, ep, id, "elsewhere.mp3"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UnsubscribeFeed(ctx, f.other, f.admin); err != nil {
		t.Fatal(err)
	}
	if f.rowExists(t, id) {
		t.Fatal("row should be deleted")
	}
	if !fileExists(outside) {
		t.Fatal("a file outside the media root was deleted")
	}
}

// TestUndoDownload_TidiesFolderAfterUnsubscribe covers a download that had
// created its media when the unsubscribe ran: the unsubscribe saw the row
// and kept the folder, so the failed link must tidy it.
func TestUndoDownload_TidiesFolderAfterUnsubscribe(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx := context.Background()
	feed, _ := f.store.GetFeedByID(ctx, f.other)
	set, _ := f.store.GetSetByID(ctx, feed.SetID)
	// The feed is already gone; the late download's file is the only
	// episode left next to the feed artwork.
	if err := f.store.DeleteFeed(ctx, f.other); err != nil {
		t.Fatal(err)
	}
	if err := f.store.HardDeleteMedia(ctx, f.otherMedia); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.otherFile); err != nil {
		t.Fatal(err)
	}
	late := filepath.Join(f.root, "podcast", "Other", "late.mp3")
	if err := os.WriteFile(late, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	f.svc.undoDownload(ctx, set, late, nil, true)
	if fileExists(late) || fileExists(f.otherCoverImg) || fileExists(filepath.Dir(late)) {
		t.Fatal("late download, artwork or folder left behind")
	}
}

func TestRelPathIn(t *testing.T) {
	set := &model.Set{RootPath: "podcast"}
	root := "/media"
	for in, want := range map[string]string{
		"/media/podcast/Show/a.mp3":  "Show/a.mp3",
		"/media/podcast/..foo/a.mp3": "..foo/a.mp3",
		"/media/other/a.mp3":         "",
		"/media":                     "",
	} {
		if got := relPathIn(root, set, in); got != want {
			t.Errorf("relPathIn(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestUnsubscribeFeed_FinishesAfterRequestCancel: once the feed is deleted,
// a cancelled request context must not stop the cleanup.
func TestUnsubscribeFeed_FinishesAfterRequestCancel(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	store := &cancelAfterDeleteFeed{SQLite: f.store, cancel: cancel}
	f.svc.store = store
	if err := f.svc.UnsubscribeFeed(ctx, f.other, f.admin); err != nil {
		t.Fatal(err)
	}
	if f.rowExists(t, f.otherMedia) || fileExists(f.otherFile) {
		t.Fatal("cleanup stopped when the request was cancelled")
	}
}

// cancelAfterDeleteFeed cancels the request context right after the feed is
// deleted, like a client disconnecting at the worst moment.
type cancelAfterDeleteFeed struct {
	*repository.SQLite
	cancel context.CancelFunc
}

func (c *cancelAfterDeleteFeed) DeleteFeed(ctx context.Context, id int64) error {
	err := c.SQLite.DeleteFeed(ctx, id)
	c.cancel()
	return err
}

// TestDownloadEpisode_CleansUpAfterRequestCancel: a request cancelled while
// the download is being recorded must still remove the new row and file.
func TestDownloadEpisode_CleansUpAfterRequestCancel(t *testing.T) {
	f := newUnsubscribeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("audio"))
	}))
	defer server.Close()
	episode, err := f.store.CreateEpisode(ctx, &model.PodcastEpisode{FeedID: f.other, GUID: "cancel", Title: "cancel", EpisodeURL: server.URL + "/cancel.mp3", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.store = &cancelOnLink{SQLite: f.store, cancel: cancel}

	if _, err := f.svc.DownloadEpisode(ctx, episode, f.admin); err == nil {
		t.Fatal("expected the failed link to be reported")
	}
	all, err := f.store.ListMedia(context.Background(), repository.MediaFilter{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if m.FileName != "a.mp3" && m.FileName != "b.mp3" && m.FileName != "c.mp3" {
			t.Fatalf("row %q left behind after a cancelled download", m.RelPath)
		}
	}
	matches, _ := filepath.Glob(filepath.Join(f.root, "podcast", "Other", "*cancel*"))
	if len(matches) != 0 {
		t.Fatalf("file left behind: %v", matches)
	}
}

// cancelOnLink cancels the request context and fails while the download is
// being linked to its episode, like a client disconnecting at that moment.
type cancelOnLink struct {
	*repository.SQLite
	cancel context.CancelFunc
}

func (c *cancelOnLink) UpdateEpisodeMedia(ctx context.Context, episodeID, mediaID int64, fileName string) error {
	c.cancel()
	return context.Canceled
}
