package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/probe"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/thumb"
)

func TestBeetsArtRescan(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	album := filepath.Join(root, "Music", "Artist", "Album")
	disc := filepath.Join(album, "CD1")
	if err := os.MkdirAll(disc, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("media"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	track := filepath.Join(disc, "track.mp3")
	write(track)
	back := filepath.Join(album, "back.jpg")
	write(back)
	store, err := repository.Open(filepath.Join(t.TempDir(), "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	prober := &probe.MockProber{ProbeFunc: func(context.Context, string) (*model.Metadata, error) { return &model.Metadata{}, nil }}
	s := NewFSScanner(store, prober, &thumb.MockGenerator{}, clock.RealClock{}, root)
	s.workers = 1
	var trackID int64
	check := func(want string) {
		t.Helper()
		if err := s.Scan(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		rows, err := store.ListMedia(ctx, repository.MediaFilter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.AbsPath != track {
				continue
			}
			if row.ThumbnailPath != want {
				t.Fatalf("thumbnail = %q, want %q", row.ThumbnailPath, want)
			}
			if trackID != 0 && trackID != row.ID {
				t.Fatal("rescan recreated track")
			}
			trackID = row.ID
			return
		}
		t.Fatal("track missing")
	}
	check(back)
	// Beets adds cover.jpg later; it must supersede the already stored back.jpg.
	cover := filepath.Join(album, "cover.jpg")
	write(cover)
	check(cover)
	check(cover)
	// An album image in the nearest directory wins over a parent's artwork.
	front := filepath.Join(disc, "Front.PNG")
	write(front)
	check(front)
	if err := os.Remove(front); err != nil {
		t.Fatal(err)
	}
	check(cover)
	if err := os.Remove(cover); err != nil {
		t.Fatal(err)
	}
	check(back)
	if err := os.Remove(back); err != nil {
		t.Fatal(err)
	}
	check("")
	// Tracks indexed without art pick up the next Beets sweep too.
	write(cover)
	check(cover)
	if _, err := os.Stat(cover); err != nil {
		t.Fatal("scanner removed shared art:", err)
	}
	if _, err := os.Stat(thumb.ThumbnailPathFor(track)); !os.IsNotExist(err) {
		t.Fatalf("scanner generated a separate audio thumbnail: %v", err)
	}
}

func TestCoverPriorityIndependentOfWalkOrder(t *testing.T) {
	names := []string{"back.jpg", ".cover.jpg", "album.png", "folder.jpg", "front.jpg", "cover.png", "COVER.JPG"}
	for _, reverse := range []bool{false, true} {
		selected := ""
		for i := range names {
			if reverse {
				i = len(names) - 1 - i
			}
			if selected == "" || preferredCover(names[i], selected) {
				selected = names[i]
			}
		}
		if selected != "COVER.JPG" {
			t.Fatalf("selected %q, want Beets cover", selected)
		}
	}
}

func TestAudioArtPreservesInaccessibleSelection(t *testing.T) {
	ctx := context.Background()
	store := repository.NewMockStore()
	updates := 0
	store.MediaRepo.UpdateMediaThumbnailFunc = func(context.Context, int64, string) error { updates++; return nil }
	s := NewFSScanner(store, &probe.MockProber{}, &thumb.MockGenerator{}, clock.RealClock{}, "/media")
	s.fs = inaccessibleArtFS{}
	s.updateAudioThumbnails(ctx, []model.Media{{ID: 1, Type: model.MediaTypeAudio, RelPath: "album/track.mp3", ThumbnailPath: "/media/Music/album/cover.jpg"}}, nil, "/media/Music", nil)
	if updates != 0 {
		t.Fatal("inaccessible cover reference was cleared")
	}
}

func TestAudioArtPreservesSkippedAlbum(t *testing.T) {
	store := repository.NewMockStore()
	store.MediaRepo.UpdateMediaThumbnailFunc = func(context.Context, int64, string) error {
		t.Fatal("skipped album artwork was replaced by an ancestor image")
		return nil
	}
	s := NewFSScanner(store, &probe.MockProber{}, &thumb.MockGenerator{}, clock.RealClock{}, "/media")
	s.updateAudioThumbnails(context.Background(), []model.Media{{ID: 1, Type: model.MediaTypeAudio, RelPath: "album/track.mp3", ThumbnailPath: "/media/Music/album/cover.jpg"}}, map[string]string{"/media/Music": "unrelated.jpg"}, "/media/Music", map[string]error{"/media/Music/album": os.ErrPermission})
}

type inaccessibleArtFS struct{ osFS }

func (inaccessibleArtFS) Stat(string) (os.FileInfo, error) { return nil, os.ErrPermission }
