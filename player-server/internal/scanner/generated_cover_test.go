package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

func TestGeneratedCoverRescan(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	setPath := filepath.Join(root, "photos")
	if err := os.MkdirAll(filepath.Join(setPath, "album"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"photo.jpg", "track.mp3", filepath.Join("album", "cover.jpg")} {
		if err := os.WriteFile(filepath.Join(setPath, name), []byte("media"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	store, err := repository.Open(filepath.Join(t.TempDir(), "player.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	prober := &probe.MockProber{ProbeFunc: func(context.Context, string) (*model.Metadata, error) {
		return &model.Metadata{}, nil
	}}
	scanner := NewFSScanner(store, prober, &thumb.MockGenerator{}, clock.RealClock{}, root)
	scanner.workers = 2
	if err := scanner.Scan(ctx, root, nil); err != nil {
		t.Fatal(err)
	}
	sets, err := store.ListSets(ctx)
	if err != nil || len(sets) != 1 {
		t.Fatalf("sets after first scan: %v, %v", sets, err)
	}
	setID := sets[0].ID
	assertActiveMedia := func() {
		t.Helper()
		media, err := store.ListMedia(ctx, repository.MediaFilter{SetID: &setID})
		if err != nil {
			t.Fatal(err)
		}
		if len(media) != 3 {
			t.Fatalf("active media count = %d, want 3: %+v", len(media), media)
		}
		want := map[string]bool{"photo.jpg": false, "track.mp3": false, "album/cover.jpg": false}
		for _, item := range media {
			if _, ok := want[item.RelPath]; !ok {
				t.Fatalf("unexpected active media %q", item.RelPath)
			}
			want[item.RelPath] = true
		}
		for path, seen := range want {
			if !seen {
				t.Errorf("real media %q was lost", path)
			}
		}
	}
	assertActiveMedia()

	// Regeneration writes this file, but it is cover artwork rather than media.
	generatedPath := filepath.Join(setPath, ".cover.jpg")
	if err := os.WriteFile(generatedPath, []byte("generated cover"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := scanner.Scan(ctx, root, nil); err != nil {
		t.Fatal(err)
	}
	assertActiveMedia()
	if got := newFileDiscoverer(osFS{}).gatherCoverImages(setPath)[setPath]; got != ".cover.jpg" {
		t.Fatalf("generated cover discovery = %q, want .cover.jpg", got)
	}
	// Simulate a row imported by an older scanner. A rescan must hide only
	// that generated row, leaving the file and ordinary media untouched.
	legacyID, err := store.CreateMedia(ctx, &model.Media{
		SetID: setID, RelPath: ".cover.jpg", FileName: ".cover.jpg",
		AbsPath: generatedPath, Type: model.MediaTypeImage, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := os.WriteFile(generatedPath, []byte("regenerated cover"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := scanner.Scan(ctx, root, nil); err != nil {
			t.Fatal(err)
		}
		assertActiveMedia()
	}
	allMedia, err := store.ListMedia(ctx, repository.MediaFilter{SetID: &setID, IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(allMedia) != 4 {
		t.Fatalf("all media count = %d, want 4", len(allMedia))
	}
	for _, item := range allMedia {
		if item.ID == legacyID && item.DeletedAt == nil {
			t.Fatal("legacy generated row was not soft-deleted")
		}
	}
	if _, err := os.Stat(generatedPath); err != nil {
		t.Fatalf("generated cover was removed from disk: %v", err)
	}
}
