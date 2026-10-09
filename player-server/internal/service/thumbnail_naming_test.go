package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// writingThumbGen writes "thumb of <input>" to the output path so a test can
// tell which source a thumbnail file on disk was generated from.
func writingThumbGen() *mockThumbGenerator {
	return &mockThumbGenerator{GenerateFunc: func(_ context.Context, in, out string, _ float64) error {
		return os.WriteFile(out, []byte("thumb of "+in), 0o644)
	}}
}

type updateRecorder struct{ updates int }

func (u *updateRecorder) UpdateMedia(context.Context, *model.Media) error {
	u.updates++
	return nil
}

// TestImportMediaFile_SameStemGetsOwnThumbnail is the upload/podcast side of
// the stem collision: holiday.mp4 and holiday.png in one folder used to
// write the same holiday.jpg.
func TestImportMediaFile_SameStemGetsOwnThumbnail(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	video := &model.Media{AbsPath: filepath.Join(dir, "holiday.mp4"), Type: model.MediaTypeVideo}
	image := &model.Media{AbsPath: filepath.Join(dir, "holiday.png"), Type: model.MediaTypeImage}
	store := &updateRecorder{}
	for _, m := range []*model.Media{video, image} {
		if err := ImportMediaFile(ctx, store, m, &mockProber{}, writingThumbGen()); err != nil {
			t.Fatalf("import %s: %v", m.AbsPath, err)
		}
	}

	if video.ThumbnailPath == image.ThumbnailPath {
		t.Fatalf("both files share thumbnail %q", video.ThumbnailPath)
	}
	if want := filepath.Join(dir, ".thumbnails", "holiday.mp4.jpg"); video.ThumbnailPath != want {
		t.Errorf("video thumbnail = %q, want %q", video.ThumbnailPath, want)
	}
	for _, m := range []*model.Media{video, image} {
		got, err := os.ReadFile(m.ThumbnailPath)
		if err != nil || string(got) != "thumb of "+m.AbsPath {
			t.Errorf("thumbnail of %s holds %q (err %v)", m.AbsPath, got, err)
		}
	}
	if store.updates != 2 {
		t.Errorf("updates = %d, want 2", store.updates)
	}
}

func TestImportMediaFile_ThumbnailNegativeCases(t *testing.T) {
	ctx := context.Background()
	failing := &mockThumbGenerator{GenerateFunc: func(context.Context, string, string, float64) error {
		return errors.New("ffmpeg boom")
	}}
	tests := []struct {
		name      string
		file      string
		mediaType model.MediaType
		gen       *mockThumbGenerator
		wantErr   bool
		wantThumb string // relative to the media directory; "" for none
	}{
		{"svg is its own thumbnail", "logo.svg", model.MediaTypeImage, writingThumbGen(), false, "logo.svg"},
		{"audio gets no thumbnail", "song.mp3", model.MediaTypeAudio, writingThumbGen(), false, ""},
		{"generator failure leaves no path behind", "clip.mp4", model.MediaTypeVideo, failing, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			m := &model.Media{AbsPath: filepath.Join(dir, tt.file), Type: tt.mediaType}
			store := &updateRecorder{}
			err := ImportMediaFile(ctx, store, m, &mockProber{}, tt.gen)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			want := ""
			if tt.wantThumb != "" {
				want = filepath.Join(dir, tt.wantThumb)
			}
			if m.ThumbnailPath != want {
				t.Errorf("thumbnail = %q, want %q", m.ThumbnailPath, want)
			}
			if tt.wantErr && store.updates != 0 {
				t.Errorf("media row updated despite the failure")
			}
		})
	}
}

// TestRegenerateThumbnail_Destination checks where a regenerated thumbnail
// is written for each kind of stored path, relative to a set directory.
func TestRegenerateThumbnail_Destination(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		source string
		stored string // "" for none
		want   string
	}{
		{"no thumbnail yet: beside the source", "a/video.mp4", "", "a/.thumbnails/video.mp4.jpg"},
		{"upload thumbnail is overwritten in place", "a/video.mp4", "a/.thumbnails/video.mp4.jpg", "a/.thumbnails/video.mp4.jpg"},
		{"scanner thumbnail is overwritten in place", "a/video.mp4", ".thumbnails/a/video.mp4.jpg", ".thumbnails/a/video.mp4.jpg"},
		{"old stem name moves to the current name, same tree", "a/video.mp4", ".thumbnails/video.jpg", ".thumbnails/a/video.mp4.jpg"},
		{"old stem name of an upload", "a/video.mp4", "a/.thumbnails/video.jpg", "a/.thumbnails/video.mp4.jpg"},
		{"image serving as its own thumbnail is never overwritten", "a/photo.png", "a/photo.png", "a/.thumbnails/photo.png.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDir := t.TempDir()
			abs := func(rel string) string { return filepath.Join(setDir, filepath.FromSlash(rel)) }
			media := &model.Media{ID: 1, SetID: 1, AbsPath: abs(tt.source), Type: model.MediaTypeVideo}
			if filepath.Ext(tt.source) == ".png" {
				media.Type = model.MediaTypeImage
			}
			if tt.stored != "" {
				media.ThumbnailPath = abs(tt.stored)
			}
			store, saved := regenStore(media)
			svc := NewMediaService(store, newMockClock(), setDir, writingThumbGen(), &mockProber{})
			if err := svc.RegenerateThumbnail(ctx, 1, 1); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if *saved != abs(tt.want) {
				t.Fatalf("saved thumbnail = %q, want %q", *saved, abs(tt.want))
			}
			got, err := os.ReadFile(*saved)
			if err != nil || string(got) != "thumb of "+media.AbsPath {
				t.Errorf("thumbnail holds %q (err %v)", got, err)
			}
		})
	}
}

// TestRegenerateThumbnail_FailureKeepsStoredPath: a failed regeneration must
// not persist a path to a thumbnail that was never written.
func TestRegenerateThumbnail_FailureKeepsStoredPath(t *testing.T) {
	setDir := t.TempDir()
	stored := filepath.Join(setDir, ".thumbnails", "video.jpg")
	media := &model.Media{ID: 1, SetID: 1, AbsPath: filepath.Join(setDir, "video.mp4"), Type: model.MediaTypeVideo, ThumbnailPath: stored}
	store, saved := regenStore(media)
	gen := &mockThumbGenerator{GenerateFunc: func(context.Context, string, string, float64) error {
		return errors.New("ffmpeg boom")
	}}
	svc := NewMediaService(store, newMockClock(), setDir, gen, &mockProber{})
	if err := svc.RegenerateThumbnail(context.Background(), 1, 1); err == nil {
		t.Fatal("expected error")
	}
	if *saved != "" || media.ThumbnailPath != stored {
		t.Errorf("saved = %q, in-memory = %q; want nothing saved and %q kept", *saved, media.ThumbnailPath, stored)
	}
}

// regenStore returns a store in which user 1 is an admin and media is the
// only row; saved receives the thumbnail path written by UpdateMedia.
func regenStore(media *model.Media) (*repository.MockStore, *string) {
	saved := new(string)
	return &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(context.Context, int64) (*model.Media, error) { return media, nil },
			UpdateMediaFunc: func(_ context.Context, m *model.Media) error {
				*saved = m.ThumbnailPath
				return nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(_ context.Context, id int64) (*model.User, error) {
				return &model.User{ID: id, IsAdmin: true}, nil
			},
		},
		SetRepo: repository.MockSetRepo{
			GetSetByIDFunc: func(_ context.Context, id int64) (*model.Set, error) { return &model.Set{ID: id}, nil },
		},
	}, saved
}
