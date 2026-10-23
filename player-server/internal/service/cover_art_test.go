package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/thumb"
)

func addCoverMedia(t *testing.T, f readonlyFixture, rel string, typ model.MediaType, thumbnail string) (int64, string) {
	t.Helper()
	path := filepath.Join(f.setDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(rel), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := f.store.CreateMedia(context.Background(), &model.Media{
		SetID: f.set, RelPath: rel, AbsPath: path, FileName: filepath.Base(path), Type: typ,
		ThumbnailPath: thumbnail, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return id, path
}

func TestFolderFrontArtworkSelection(t *testing.T) {
	f := newReadonlyFixture(t)
	ctx := context.Background()
	bookID, _ := addCoverMedia(t, f, "Book/book.m4b", model.MediaTypeAudio, "")
	var paths []string
	for _, name := range []string{"cover.jpg", "cover.png", "Front.PNG", "folder.jpg", "album.jpg"} {
		_, path := addCoverMedia(t, f, "Book/"+name, model.MediaTypeImage, "")
		paths = append(paths, path)
	}
	browse, err := f.svc.BrowseSet(ctx, f.set, f.user, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range browse.Media {
		found = found || m.ID == bookID
	}
	if !found {
		t.Fatal("front/folder/album source artwork prevented single-book flattening")
	}
	for _, want := range paths {
		got, err := f.svc.GetSetCover(ctx, f.set, "Book", f.user)
		if err != nil || got.Path != want {
			t.Fatalf("folder cover: %+v, %v; want %s", got, err, want)
		}
		if err := os.Remove(want); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.GetSetCover(ctx, f.set, "Book", f.user); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing source artwork: %v", err)
	}
}

func TestFolderCoverAudioFallbackAndGeneratedPriority(t *testing.T) {
	f := newReadonlyFixture(t)
	ctx := context.Background()
	art := filepath.Join(f.cache, "embedded.jpg")
	other := filepath.Join(f.cache, "headspace.jpg")
	for _, path := range []string{art, other} {
		if err := os.WriteFile(path, []byte(path), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = addCoverMedia(t, f, "Book/chapter.m4b", model.MediaTypeAudio, art)
	_, _ = addCoverMedia(t, f, "Book/text.jpg", model.MediaTypeImage, other)
	_, _ = addCoverMedia(t, f, "Book/intro.mp4", model.MediaTypeVideo, other)
	_, sibling := addCoverMedia(t, f, "Book-other/cover.jpg", model.MediaTypeImage, "")
	for i := 0; i < 32; i++ {
		got, err := f.svc.GetSetCover(ctx, f.set, "Book", f.user)
		if err != nil || got.Path != art {
			t.Fatalf("audio artwork fallback: %+v, %v; sibling %s must not match", got, err, sibling)
		}
	}
	// Explicit generated artwork is intentional and retains priority over
	// newly recognized source artwork and every automatic fallback.
	_, _ = addCoverMedia(t, f, "Book/Front.PNG", model.MediaTypeImage, "")
	generated, ok := thumb.NewLayout(f.root, f.cache).CoverPathFor(filepath.Join(f.setDir, "Book"))
	if !ok {
		t.Fatal("cover destination is outside the media root")
	}
	if err := os.MkdirAll(filepath.Dir(generated), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(generated, []byte("chosen cover"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.GetSetCover(ctx, f.set, "Book", f.user)
	if err != nil || got.Path != generated {
		t.Fatalf("generated priority: %+v, %v", got, err)
	}
	// Collections without book/audio art still use photo/video thumbnails.
	_, _ = addCoverMedia(t, f, "Photos/photo.jpg", model.MediaTypeImage, other)
	got, err = f.svc.GetSetCover(ctx, f.set, "Photos", f.user)
	if err != nil || got.Path != other {
		t.Fatalf("photo fallback: %+v, %v", got, err)
	}
	_, _ = addCoverMedia(t, f, "Videos/clip.mp4", model.MediaTypeVideo, other)
	got, err = f.svc.GetSetCover(ctx, f.set, "Videos", f.user)
	if err != nil || got.Path != other {
		t.Fatalf("video fallback: %+v, %v", got, err)
	}
}

func TestRegenerateCoverUsesPreferredSourceOrAudioArtwork(t *testing.T) {
	f := newReadonlyFixture(t)
	ctx := context.Background()
	_, front := addCoverMedia(t, f, "Book/Front.PNG", model.MediaTypeImage, "")
	_, _ = addCoverMedia(t, f, "Book/folder.jpg", model.MediaTypeImage, "")
	_, _ = addCoverMedia(t, f, "Book/back.jpg", model.MediaTypeImage, "")
	_, _ = addCoverMedia(t, f, "Book/intro.mp4", model.MediaTypeVideo, "")
	var source string
	gen := &mockThumbGenerator{GenerateFunc: func(_ context.Context, input, output string, _ float64) error {
		source = input
		return os.WriteFile(output, []byte("generated"), 0o644)
	}}
	svc := NewMediaService(f.store, newMockClock(), f.root, gen, &mockProber{}).WithThumbnailLayout(thumb.NewLayout(f.root, f.cache))
	for i := 0; i < 16; i++ {
		if err := svc.RegenerateSetCover(ctx, f.set, "Book", f.user); err != nil {
			t.Fatal(err)
		}
		if source != front {
			t.Fatalf("regenerated from %s, want %s", source, front)
		}
	}
	art := filepath.Join(f.cache, "embedded.jpg")
	if err := os.WriteFile(art, []byte("embedded front"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _ = addCoverMedia(t, f, "Embedded/book.m4b", model.MediaTypeAudio, art)
	_, _ = addCoverMedia(t, f, "Embedded/back.jpg", model.MediaTypeImage, "")
	_, _ = addCoverMedia(t, f, "Embedded/intro.mp4", model.MediaTypeVideo, "")
	if err := svc.RegenerateSetCover(ctx, f.set, "Embedded", f.user); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetSetCover(ctx, f.set, "Embedded", f.user)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(got.Path)
	if err != nil || string(data) != "embedded front" {
		t.Fatalf("regenerated embedded cover: %q, %v", data, err)
	}
}
