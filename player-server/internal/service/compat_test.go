package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/transcode"
)

// fakeRenditions is a RenditionProvider that records its calls.
type fakeRenditions struct {
	calls []transcode.Source
	err   error
}

func (f *fakeRenditions) Ensure(_ context.Context, src transcode.Source) (transcode.Rendition, error) {
	f.calls = append(f.calls, src)
	if f.err != nil {
		return transcode.Rendition{}, f.err
	}
	return transcode.Rendition{Path: "/cache/r.mp4", ContentType: "video/mp4", ETag: "tag"}, nil
}

// compatStore returns a store with one media item; user 1 is an admin, user 2
// has no permission on the item's set.
func compatStore(media *model.Media) *repository.MockStore {
	return &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(_ context.Context, id int64) (*model.Media, error) {
				if media == nil || id != media.ID {
					return nil, nil
				}
				return media, nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(_ context.Context, id int64) (*model.User, error) {
				return &model.User{ID: id, IsAdmin: id == 1}, nil
			},
		},
	}
}

func statusOf(err error) int {
	var s interface{ HTTPStatus() int }
	if errors.As(err, &s) {
		return s.HTTPStatus()
	}
	return 0
}

// mediaTree creates a media root with real files, because the service
// resolves symlinks before it hands a path to the transcoder. It returns the
// (symlink-free) root.
func mediaTree(t *testing.T, files ...string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		path := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("media"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCompatStream_Success(t *testing.T) {
	root := mediaTree(t, "set/a.avi", "set/a.wma")
	tests := []struct {
		name     string
		media    *model.Media
		wantKind transcode.Kind
	}{
		{"video", &model.Media{ID: 5, Type: model.MediaTypeVideo, FileName: "a.avi", AbsPath: filepath.Join(root, "set/a.avi")}, transcode.KindVideo},
		{"audio", &model.Media{ID: 5, Type: model.MediaTypeAudio, FileName: "a.wma", AbsPath: filepath.Join(root, "set/a.wma")}, transcode.KindAudio},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renditions := &fakeRenditions{}
			svc := NewCompatStreamService(NewAccessHelper(compatStore(tt.media)), nil, renditions, root)
			got, err := svc.CompatStream(context.Background(), 5, 1)
			if err != nil || got == nil || got.Path != "/cache/r.mp4" {
				t.Fatalf("CompatStream = %+v, %v", got, err)
			}
			want := transcode.Source{MediaID: 5, Path: tt.media.AbsPath, Kind: tt.wantKind, Requester: "user:1"}
			if len(renditions.calls) != 1 || renditions.calls[0] != want {
				t.Errorf("source = %+v, want %+v", renditions.calls, want)
			}
		})
	}
}

// rejection is a request the service must refuse before any transcode starts.
type rejection struct {
	name       string
	media      *model.Media
	mediaID    int64
	userID     int64
	wantErr    error
	wantStatus int
}

func compatRejections(t *testing.T, root string) []rejection {
	t.Helper()
	deleted := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	avi := filepath.Join(root, "set/a.avi")
	// A link inside the media root that points at a file outside of it.
	secret := filepath.Join(t.TempDir(), "secret.avi")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "set/link.avi")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	video := func(m model.Media) *model.Media {
		m.ID, m.SetID, m.Type = 5, 1, model.MediaTypeVideo
		return &m
	}
	return []rejection{
		{"no access", video(model.Media{FileName: "a.avi", AbsPath: avi}), 5, 2, ErrForbidden, http.StatusForbidden},
		{"unknown id", video(model.Media{FileName: "a.avi", AbsPath: avi}), 99, 1, ErrNotFound, http.StatusNotFound},
		{"soft deleted", video(model.Media{FileName: "a.avi", AbsPath: avi, DeletedAt: &deleted}), 5, 1, ErrNotFound, http.StatusNotFound},
		{"image media", &model.Media{ID: 5, Type: model.MediaTypeImage, FileName: "a.jpg", AbsPath: filepath.Join(root, "set/a.jpg")}, 5, 1, ErrNotTranscodable, http.StatusUnsupportedMediaType},
		// Media that plays natively is not transcoded on request.
		{"native mp4", video(model.Media{FileName: "a.mp4", Codec: "h264/aac", AbsPath: filepath.Join(root, "set/a.mp4")}), 5, 1, ErrCompatNotNeeded, http.StatusBadRequest},
		{"path outside media root", video(model.Media{FileName: "secret.avi", AbsPath: secret}), 5, 1, ErrForbidden, http.StatusForbidden},
		{"symlink leaving media root", video(model.Media{FileName: "link.avi", AbsPath: link}), 5, 1, ErrForbidden, http.StatusForbidden},
		{"file missing on disk", video(model.Media{FileName: "gone.avi", AbsPath: filepath.Join(root, "set/gone.avi")}), 5, 1, ErrNotFound, http.StatusNotFound},
	}
}

func TestCompatStream_Rejections(t *testing.T) {
	root := mediaTree(t, "set/a.avi", "set/a.jpg", "set/a.mp4")
	for _, tt := range compatRejections(t, root) {
		t.Run(tt.name, func(t *testing.T) {
			renditions := &fakeRenditions{}
			svc := NewCompatStreamService(NewAccessHelper(compatStore(tt.media)), nil, renditions, root)
			_, err := svc.CompatStream(context.Background(), tt.mediaID, tt.userID)
			if !errors.Is(err, tt.wantErr) || statusOf(err) != tt.wantStatus {
				t.Fatalf("error = %v (status %d), want %v (status %d)", err, statusOf(err), tt.wantErr, tt.wantStatus)
			}
			if len(renditions.calls) != 0 {
				t.Error("the transcoder must not be reached")
			}
			// These errors are shown to clients: no server paths in them.
			if strings.Contains(err.Error(), root) {
				t.Errorf("error leaks a server path: %v", err)
			}
		})
	}
}

func TestCompatStream_ProviderErrors(t *testing.T) {
	root := mediaTree(t, "set/a.avi")
	media := &model.Media{ID: 5, Type: model.MediaTypeVideo, FileName: "a.avi", AbsPath: filepath.Join(root, "set/a.avi")}
	withPath := func(sentinel error) error { return fmt.Errorf("%w: stat %s: no such file", sentinel, media.AbsPath) }
	ffmpegErr := errors.New("ffmpeg transcode: exit status 1")

	tests := []struct {
		name       string
		ensureErr  error
		wantErr    error
		wantStatus int    // 0: not a client-facing sentinel, the API answers 500
		wantText   string // exact client-facing text for sentinels
	}{
		{"still transcoding", transcode.ErrPending, ErrTranscodePending, http.StatusServiceUnavailable, "transcode in progress"},
		{"source changed mid-run", withPath(transcode.ErrSourceChanged), ErrTranscodePending, http.StatusServiceUnavailable, "transcode in progress"},
		{"transcoder busy", transcode.ErrBusy, ErrTranscodeBusy, http.StatusServiceUnavailable, "transcoder busy"},
		{"disk full", withPath(transcode.ErrNoSpace), ErrTranscodeNoSpace, http.StatusInsufficientStorage, "insufficient storage for transcode"},
		{"source file missing", withPath(transcode.ErrSourceMissing), ErrNotFound, http.StatusNotFound, "not found"},
		{"ffmpeg failure", ffmpegErr, ffmpegErr, 0, ""},
		{"failed recently", transcode.ErrFailedRecently, transcode.ErrFailedRecently, 0, ""},
		{"aborted by shutdown", transcode.ErrAborted, transcode.ErrAborted, 0, ""},
		{"cancelled request", context.Canceled, context.Canceled, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewCompatStreamService(NewAccessHelper(compatStore(media)), nil, &fakeRenditions{err: tt.ensureErr}, root)
			_, err := svc.CompatStream(context.Background(), 5, 1)
			if !errors.Is(err, tt.wantErr) || statusOf(err) != tt.wantStatus {
				t.Fatalf("error = %v (status %d), want %v (status %d)", err, statusOf(err), tt.wantErr, tt.wantStatus)
			}
			if tt.wantStatus != 0 && err.Error() != tt.wantText {
				t.Errorf("client-facing text = %q, want exactly %q", err.Error(), tt.wantText)
			}
		})
	}
}

// sharedCase is one SharedCompatStream scenario.
type sharedCase struct {
	name      string
	share     *model.Share
	media     *model.Media
	ensureErr error
	wantErr   error // nil: success
	wantUses  int
}

func sharedCases(root string) []sharedCase {
	now := newMockClock().T
	video := &model.Media{ID: 5, Type: model.MediaTypeVideo, FileName: "a.flv", AbsPath: filepath.Join(root, "set/a.flv")}
	image := &model.Media{ID: 5, Type: model.MediaTypeImage, FileName: "a.png", AbsPath: filepath.Join(root, "set/a.png")}
	native := &model.Media{ID: 5, Type: model.MediaTypeVideo, FileName: "a.mp4", AbsPath: filepath.Join(root, "set/a.mp4")}
	one := 1
	valid := &model.Share{Token: "tok", MediaID: 5, ExpiresAt: now.Add(time.Hour)}
	expired := &model.Share{Token: "tok", MediaID: 5, ExpiresAt: now.Add(-time.Hour)}
	usedUp := &model.Share{Token: "tok", MediaID: 5, ExpiresAt: now.Add(time.Hour), MaxUses: &one, UsedCount: 1}
	boom := errors.New("ffmpeg transcode: exit status 1")
	return []sharedCase{
		{name: "ok counts one use", share: valid, media: video, wantUses: 1},
		{name: "unknown token", media: video, wantErr: ErrShareNotFound},
		{name: "expired", share: expired, media: video, wantErr: ErrShareExpired},
		{name: "max uses reached", share: usedUp, media: video, wantErr: ErrShareExpired},
		{name: "media gone", share: valid, wantErr: ErrMediaNotFound},
		{name: "image media", share: valid, media: image, wantErr: ErrNotTranscodable},
		{name: "native media", share: valid, media: native, wantErr: ErrCompatNotNeeded},
		// Answers without content must not consume share uses.
		{name: "pending does not count a use", share: valid, media: video, ensureErr: transcode.ErrPending, wantErr: ErrTranscodePending},
		{name: "busy does not count a use", share: valid, media: video, ensureErr: transcode.ErrBusy, wantErr: ErrTranscodeBusy},
		{name: "ffmpeg failure does not count a use", share: valid, media: video, ensureErr: boom, wantErr: boom},
	}
}

func TestSharedCompatStream(t *testing.T) {
	root := mediaTree(t, "set/a.flv", "set/a.png", "set/a.mp4")
	for _, tt := range sharedCases(root) {
		t.Run(tt.name, func(t *testing.T) {
			uses := 0
			store := compatStore(tt.media)
			store.ShareRepo = repository.MockShareRepo{
				GetShareByTokenFunc: func(context.Context, string) (*model.Share, error) { return tt.share, nil },
				UseShareFunc: func(context.Context, string, time.Time) (bool, error) {
					uses++
					return true, nil
				},
			}
			helper := NewAccessHelper(store)
			renditions := &fakeRenditions{err: tt.ensureErr}
			svc := NewCompatStreamService(helper, NewShareService(store, newMockClock(), helper), renditions, root)

			got, err := svc.SharedCompatStream(context.Background(), "tok")
			if uses != tt.wantUses {
				t.Errorf("share uses = %d, want %d", uses, tt.wantUses)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && (got == nil || renditions.calls[0].Requester != "share:tok") {
				t.Errorf("rendition %+v, source %+v", got, renditions.calls)
			}
		})
	}
}

func TestSharedCompatStream_ShareUsedUpWhileTranscoding(t *testing.T) {
	// The share's last use is taken by another request while the rendition
	// is being produced: the atomic UseShare then refuses.
	root := mediaTree(t, "a.avi")
	now := newMockClock().T
	store := compatStore(&model.Media{ID: 5, Type: model.MediaTypeVideo, FileName: "a.avi", AbsPath: filepath.Join(root, "a.avi")})
	store.ShareRepo = repository.MockShareRepo{
		GetShareByTokenFunc: func(context.Context, string) (*model.Share, error) {
			return &model.Share{Token: "tok", MediaID: 5, ExpiresAt: now.Add(time.Hour)}, nil
		},
		UseShareFunc: func(context.Context, string, time.Time) (bool, error) { return false, nil },
	}
	helper := NewAccessHelper(store)
	svc := NewCompatStreamService(helper, NewShareService(store, newMockClock(), helper), &fakeRenditions{}, root)
	if _, err := svc.SharedCompatStream(context.Background(), "tok"); !errors.Is(err, ErrShareExpired) {
		t.Fatalf("error = %v, want ErrShareExpired", err)
	}
}

func TestConsumeShareUse_StoreError(t *testing.T) {
	store := &repository.MockStore{ShareRepo: repository.MockShareRepo{
		UseShareFunc: func(context.Context, string, time.Time) (bool, error) { return false, errors.New("db down") },
	}}
	err := NewShareService(store, newMockClock(), NewAccessHelper(store)).ConsumeShareUse(context.Background(), "tok")
	if err == nil || errors.Is(err, ErrShareExpired) {
		t.Fatalf("error = %v, want the store error", err)
	}
}

func TestResolveSourcePath(t *testing.T) {
	root := mediaTree(t, "set/a.avi", "other/b.avi")
	// A symlinked directory inside the root that stays inside is fine.
	if err := os.Symlink(filepath.Join(root, "other"), filepath.Join(root, "set/alias")); err != nil {
		t.Fatal(err)
	}
	// The media root itself may be a symlink (e.g. to a mounted volume).
	rootLink := filepath.Join(t.TempDir(), "media")
	if err := os.Symlink(root, rootLink); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, root, path, want string
		wantErr                error
	}{
		{"plain file", root, filepath.Join(root, "set/a.avi"), filepath.Join(root, "set/a.avi"), nil},
		{"link staying inside", root, filepath.Join(root, "set/alias/b.avi"), filepath.Join(root, "other/b.avi"), nil},
		{"symlinked root", rootLink, filepath.Join(rootLink, "set/a.avi"), filepath.Join(root, "set/a.avi"), nil},
		{"dot-dot escape", root, filepath.Join(root, "set/../../etc/passwd"), "", ErrNotFound},
		{"missing file", root, filepath.Join(root, "set/none.avi"), "", ErrNotFound},
		{"missing root", filepath.Join(root, "absent"), filepath.Join(root, "set/a.avi"), "", ErrNotFound},
		{"no root configured", "", "/anything/at/all", "/anything/at/all", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSourcePath(tt.root, tt.path)
			if got != tt.want || !errors.Is(err, tt.wantErr) {
				t.Errorf("resolveSourcePath = %q, %v; want %q, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestPathWithinRoot(t *testing.T) {
	tests := []struct {
		root, path string
		want       bool
	}{
		{"", "/anything", true},
		{"/media", "/media/set/a.avi", true},
		{"/media/", "/media/set/a.avi", true},
		{"/media", "/media/../etc/passwd", false},
		{"/media", "/mediaevil/a.avi", false},
		{"/media", "/media", false},
		{"/media", "", false},
	}
	for _, tt := range tests {
		if got := pathWithinRoot(tt.root, tt.path); got != tt.want {
			t.Errorf("pathWithinRoot(%q, %q) = %v, want %v", tt.root, tt.path, got, tt.want)
		}
	}
}

func TestBuildPlaybackHint_PlaybackURL(t *testing.T) {
	tests := []struct {
		name       string
		media      model.Media
		wantURL    string
		transcoded bool
	}{
		{"native mp4", model.Media{ID: 42, Type: model.MediaTypeVideo, FileName: "a.mp4", Codec: "h264/aac"}, "/api/v1/media/42/stream", false},
		// mkv sets the broad needs_transcode heuristic but plays natively.
		{"mkv stays on stream", model.Media{ID: 42, Type: model.MediaTypeVideo, FileName: "a.mkv", Codec: "h264"}, "/api/v1/media/42/stream", false},
		{"avi", model.Media{ID: 42, Type: model.MediaTypeVideo, FileName: "a.avi", Codec: "mpeg4"}, "/api/v1/media/42/compat", true},
		{"wma", model.Media{ID: 42, Type: model.MediaTypeAudio, FileName: "a.wma", Codec: "wmav2"}, "/api/v1/media/42/compat", true},
		{"mkv with ac3 audio", model.Media{ID: 42, Type: model.MediaTypeVideo, FileName: "a.mkv", Codec: "h264/ac3"}, "/api/v1/media/42/compat", true},
		{"image", model.Media{ID: 42, Type: model.MediaTypeImage, FileName: "a.jpg"}, "/api/v1/media/42/stream", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hint := buildPlaybackHint(&tt.media)
			if hint.PlaybackURL != tt.wantURL || hint.Transcoded != tt.transcoded {
				t.Errorf("playback_url=%q transcoded=%v, want %q %v", hint.PlaybackURL, hint.Transcoded, tt.wantURL, tt.transcoded)
			}
			if hint.StreamURL != "/api/v1/media/42/stream" {
				t.Errorf("stream_url must always be the original, got %q", hint.StreamURL)
			}
		})
	}
}

func TestGetSharedMedia_PlaybackURL(t *testing.T) {
	now := newMockClock().T
	tests := []struct {
		name       string
		media      *model.Media
		wantURL    string
		transcoded bool
	}{
		{"native", &model.Media{ID: 1, Type: model.MediaTypeVideo, FileName: "a.mp4"}, "/s/tok/stream", false},
		{"legacy", &model.Media{ID: 1, Type: model.MediaTypeVideo, FileName: "a.wmv"}, "/s/tok/compat", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := compatStore(tt.media)
			store.ShareRepo = repository.MockShareRepo{
				GetShareByTokenFunc: func(context.Context, string) (*model.Share, error) {
					return &model.Share{Token: "tok", MediaID: 1, ExpiresAt: now.Add(time.Hour)}, nil
				},
			}
			res, err := NewShareService(store, newMockClock(), NewAccessHelper(store)).GetSharedMedia(context.Background(), "tok")
			if err != nil {
				t.Fatalf("GetSharedMedia: %v", err)
			}
			if res.PlaybackURL != tt.wantURL || res.Transcoded != tt.transcoded || res.StreamURL != "/s/tok/stream" {
				t.Errorf("got playback_url=%q transcoded=%v stream_url=%q", res.PlaybackURL, res.Transcoded, res.StreamURL)
			}
		})
	}
}

// fakeRenditionGC records what the GC worker asks of the transcode cache.
type fakeRenditionGC struct {
	pruned  int
	removed []int64
	err     error
}

func (f *fakeRenditionGC) Prune(context.Context) error { f.pruned++; return f.err }
func (f *fakeRenditionGC) Remove(id int64) error       { f.removed = append(f.removed, id); return f.err }

func TestGCWorker_MaintainsRenditionCache(t *testing.T) {
	now := newMockClock().T
	old := now.Add(-8 * 24 * time.Hour)
	recent := now.Add(-time.Hour)
	trash := []model.Media{
		{ID: 1, AbsPath: "/nonexistent/old.avi", DeletedAt: &old},
		{ID: 2, AbsPath: "/nonexistent/new.avi", DeletedAt: &recent},
	}

	tests := []struct {
		name        string
		listErr     error
		cacheErr    error
		wantRemoved []int64
	}{
		{name: "removes renditions of hard-deleted media and prunes", wantRemoved: []int64{1}},
		// Cache errors are logged, never fatal for the GC run.
		{name: "cache errors are tolerated", cacheErr: errors.New("disk"), wantRemoved: []int64{1}},
		// Pruning is independent of the trash listing.
		{name: "prunes even when listing trash fails", listErr: errors.New("db")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{MediaRepo: repository.MockMediaRepo{
				ListDeletedMediaFunc: func(context.Context) ([]model.Media, error) { return trash, tt.listErr },
				HardDeleteMediaFunc:  func(context.Context, int64) error { return nil },
			}}
			cache := &fakeRenditionGC{err: tt.cacheErr}
			w := NewGCWorker(store, newMockClock(), "/media", time.Minute, nil).WithRenditionCache(cache)
			if err := w.RunOnce(); err != nil {
				t.Fatalf("RunOnce: %v", err)
			}
			if cache.pruned != 1 {
				t.Errorf("Prune calls = %d, want 1", cache.pruned)
			}
			if fmt.Sprint(cache.removed) != fmt.Sprint(tt.wantRemoved) {
				t.Errorf("removed = %v, want %v", cache.removed, tt.wantRemoved)
			}
		})
	}
}
