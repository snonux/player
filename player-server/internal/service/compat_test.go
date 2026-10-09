package service

import (
	"context"
	"errors"
	"net/http"
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

func TestCompatStream(t *testing.T) {
	deleted := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	video := &model.Media{ID: 5, SetID: 1, Type: model.MediaTypeVideo, FileName: "a.avi", AbsPath: "/media/set/a.avi"}
	audio := &model.Media{ID: 5, SetID: 1, Type: model.MediaTypeAudio, FileName: "a.wma", AbsPath: "/media/set/a.wma"}
	image := &model.Media{ID: 5, SetID: 1, Type: model.MediaTypeImage, FileName: "a.jpg", AbsPath: "/media/set/a.jpg"}
	trashed := &model.Media{ID: 5, SetID: 1, Type: model.MediaTypeVideo, AbsPath: "/media/set/a.avi", DeletedAt: &deleted}
	outside := &model.Media{ID: 5, SetID: 1, Type: model.MediaTypeVideo, AbsPath: "/etc/passwd"}
	ffmpegErr := errors.New("ffmpeg transcode /media/set/a.avi: exit status 1")

	tests := []struct {
		name       string
		media      *model.Media
		mediaID    int64
		userID     int64
		ensureErr  error
		wantErr    error // matched with errors.Is; nil means success
		wantStatus int   // expected HTTPStatus of the error, 0 = none (-> 500)
		wantKind   transcode.Kind
		wantEnsure bool
	}{
		{name: "video ok", media: video, mediaID: 5, userID: 1, wantKind: transcode.KindVideo, wantEnsure: true},
		{name: "audio ok", media: audio, mediaID: 5, userID: 1, wantKind: transcode.KindAudio, wantEnsure: true},
		{name: "no access", media: video, mediaID: 5, userID: 2, wantErr: ErrForbidden, wantStatus: http.StatusForbidden},
		{name: "unknown id", media: video, mediaID: 99, userID: 1, wantErr: ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "soft deleted", media: trashed, mediaID: 5, userID: 1, wantErr: ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "image media", media: image, mediaID: 5, userID: 1, wantErr: ErrNotTranscodable, wantStatus: http.StatusUnsupportedMediaType},
		{name: "path outside media root", media: outside, mediaID: 5, userID: 1, wantErr: ErrForbidden, wantStatus: http.StatusForbidden},
		{name: "ffmpeg failure", media: video, mediaID: 5, userID: 1, ensureErr: ffmpegErr, wantErr: ffmpegErr, wantEnsure: true},
		{name: "still transcoding", media: video, mediaID: 5, userID: 1, ensureErr: transcode.ErrPending, wantErr: ErrTranscodePending, wantStatus: http.StatusServiceUnavailable, wantEnsure: true},
		{name: "cancelled request", media: video, mediaID: 5, userID: 1, ensureErr: context.Canceled, wantErr: context.Canceled, wantEnsure: true},
		{name: "source file missing", media: video, mediaID: 5, userID: 1, ensureErr: transcode.ErrSourceMissing, wantErr: ErrNotFound, wantStatus: http.StatusNotFound, wantEnsure: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := compatStore(tt.media)
			renditions := &fakeRenditions{err: tt.ensureErr}
			svc := NewCompatStreamService(NewAccessHelper(store), nil, renditions, "/media")

			got, err := svc.CompatStream(context.Background(), tt.mediaID, tt.userID)

			if (len(renditions.calls) > 0) != tt.wantEnsure {
				t.Errorf("Ensure called = %v, want %v", len(renditions.calls) > 0, tt.wantEnsure)
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				if statusOf(err) != tt.wantStatus {
					t.Errorf("HTTP status = %d, want %d", statusOf(err), tt.wantStatus)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil || got.Path != "/cache/r.mp4" {
				t.Errorf("unexpected rendition %+v", got)
			}
			src := renditions.calls[0]
			if src.MediaID != 5 || src.Path != tt.media.AbsPath || src.Kind != tt.wantKind {
				t.Errorf("unexpected source %+v", src)
			}
		})
	}
}

func TestSharedCompatStream(t *testing.T) {
	now := newMockClock().T
	video := &model.Media{ID: 5, Type: model.MediaTypeVideo, FileName: "a.flv", AbsPath: "/media/set/a.flv"}
	image := &model.Media{ID: 5, Type: model.MediaTypeImage, FileName: "a.png", AbsPath: "/media/set/a.png"}
	one := 1
	valid := &model.Share{Token: "tok", MediaID: 5, ExpiresAt: now.Add(time.Hour)}
	expired := &model.Share{Token: "tok", MediaID: 5, ExpiresAt: now.Add(-time.Hour)}
	usedUp := &model.Share{Token: "tok", MediaID: 5, ExpiresAt: now.Add(time.Hour), MaxUses: &one, UsedCount: 1}

	tests := []struct {
		name      string
		share     *model.Share
		media     *model.Media
		ensureErr error
		wantErr   error
		wantUses  int
	}{
		{name: "ok counts one use", share: valid, media: video, wantUses: 1},
		{name: "unknown token", share: nil, media: video, wantErr: ErrShareNotFound},
		{name: "expired", share: expired, media: video, wantErr: ErrShareExpired},
		{name: "max uses reached", share: usedUp, media: video, wantErr: ErrShareExpired},
		{name: "media gone", share: valid, media: nil, wantErr: ErrMediaNotFound},
		{name: "image media", share: valid, media: image, wantErr: ErrNotTranscodable},
		// A waiting client polls; those polls must not consume share uses.
		{name: "pending does not count a use", share: valid, media: video, ensureErr: transcode.ErrPending, wantErr: ErrTranscodePending},
		{name: "ffmpeg failure does not count a use", share: valid, media: video, ensureErr: errors.New("boom"), wantErr: nil},
	}
	for _, tt := range tests {
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
			shares := NewShareService(store, newMockClock(), helper)
			renditions := &fakeRenditions{err: tt.ensureErr}
			svc := NewCompatStreamService(helper, shares, renditions, "/media")

			got, err := svc.SharedCompatStream(context.Background(), "tok")

			if uses != tt.wantUses {
				t.Errorf("share uses = %d, want %d", uses, tt.wantUses)
			}
			if tt.wantErr == nil && tt.ensureErr == nil {
				if err != nil || got == nil {
					t.Fatalf("unexpected result %+v, %v", got, err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestSharedCompatStream_ShareUsedUpWhileTranscoding(t *testing.T) {
	// The share's last use is taken by another request while the rendition
	// is being produced: the atomic UseShare then refuses.
	now := newMockClock().T
	store := compatStore(&model.Media{ID: 5, Type: model.MediaTypeVideo, AbsPath: "/media/a.avi"})
	store.ShareRepo = repository.MockShareRepo{
		GetShareByTokenFunc: func(context.Context, string) (*model.Share, error) {
			return &model.Share{Token: "tok", MediaID: 5, ExpiresAt: now.Add(time.Hour)}, nil
		},
		UseShareFunc: func(context.Context, string, time.Time) (bool, error) { return false, nil },
	}
	helper := NewAccessHelper(store)
	svc := NewCompatStreamService(helper, NewShareService(store, newMockClock(), helper), &fakeRenditions{}, "/media")
	if _, err := svc.SharedCompatStream(context.Background(), "tok"); !errors.Is(err, ErrShareExpired) {
		t.Fatalf("error = %v, want ErrShareExpired", err)
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
		{"native mp4", model.Media{ID: 42, Type: model.MediaTypeVideo, FileName: "a.mp4", Codec: "h264"}, "/api/v1/media/42/stream", false},
		// mkv sets the broad needs_transcode heuristic but plays natively.
		{"mkv stays on stream", model.Media{ID: 42, Type: model.MediaTypeVideo, FileName: "a.mkv", Codec: "h264"}, "/api/v1/media/42/stream", false},
		{"avi", model.Media{ID: 42, Type: model.MediaTypeVideo, FileName: "a.avi", Codec: "mpeg4"}, "/api/v1/media/42/compat", true},
		{"wma", model.Media{ID: 42, Type: model.MediaTypeAudio, FileName: "a.wma", Codec: "wmav2"}, "/api/v1/media/42/compat", true},
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
				ListDeletedMediaFunc: func(context.Context) ([]model.Media, error) {
					return []model.Media{
						{ID: 1, AbsPath: "/nonexistent/old.avi", DeletedAt: &old},
						{ID: 2, AbsPath: "/nonexistent/new.avi", DeletedAt: &recent},
					}, tt.listErr
				},
				HardDeleteMediaFunc: func(context.Context, int64) error { return nil },
			}}
			cache := &fakeRenditionGC{err: tt.cacheErr}
			w := NewGCWorker(store, newMockClock(), "/media", time.Minute, nil).WithRenditionCache(cache)
			if err := w.RunOnce(); err != nil {
				t.Fatalf("RunOnce: %v", err)
			}
			if cache.pruned != 1 {
				t.Errorf("Prune calls = %d, want 1", cache.pruned)
			}
			if len(cache.removed) != len(tt.wantRemoved) || (len(tt.wantRemoved) == 1 && cache.removed[0] != tt.wantRemoved[0]) {
				t.Errorf("removed = %v, want %v", cache.removed, tt.wantRemoved)
			}
		})
	}
}
