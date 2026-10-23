package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/probe"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/thumb"
)

func TestAudioArtworkCleanupGuards(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		shared, source, updateError, lookupError bool
	}{
		{name: "unused generated art is removed"},
		{name: "trashed cross-set reference keeps art", shared: true},
		{name: "indexed source image keeps art", source: true},
		{name: "failed row update keeps art", updateError: true},
		{name: "unknown references keep art", lookupError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, cache := t.TempDir(), t.TempDir()
			set := filepath.Join(root, "Music")
			track := filepath.Join(set, "album", "track.flac")
			cover := filepath.Join(set, "album", "cover.jpg")
			layout := thumb.NewLayout(root, cache)
			generated, _ := layout.PathFor(track)
			for _, path := range []string{track, cover, generated} {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("keep content"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			rows := []model.Media{{ID: 1, SetID: 1, AbsPath: track, RelPath: "album/track.flac", Type: model.MediaTypeAudio, ThumbnailPath: generated}}
			if tc.shared {
				now := time.Now()
				rows = append(rows, model.Media{ID: 2, SetID: 2, AbsPath: filepath.Join(root, "other", "song.flac"), Type: model.MediaTypeAudio, ThumbnailPath: generated, DeletedAt: &now})
			}
			if tc.source {
				rows = append(rows, model.Media{ID: 3, SetID: 1, AbsPath: generated, Type: model.MediaTypeImage})
			}
			store := repository.NewMockStore()
			store.MediaRepo.ListMediaFunc = func(_ context.Context, f repository.MediaFilter) ([]model.Media, error) {
				if !f.IncludeDeleted || f.SetID != nil {
					t.Error("reference lookup must include every set and trash")
				}
				if tc.lookupError {
					return nil, errors.New("read failed")
				}
				return rows, nil
			}
			updated := false
			store.MediaRepo.UpdateMediaThumbnailFunc = func(_ context.Context, id int64, path string) error {
				if id != 1 || path != cover {
					t.Errorf("unexpected update: %d, %q", id, path)
				}
				if tc.updateError {
					return errors.New("write failed")
				}
				updated = true
				return nil
			}
			s := NewFSScanner(store, &probe.MockProber{}, &thumb.MockGenerator{}, clock.RealClock{}, root).WithThumbnailLayout(layout)
			s.updateAudioThumbnails(context.Background(), rows[:1], map[string]string{filepath.Dir(track): "album/cover.jpg"}, set, nil)
			if updated == tc.updateError {
				t.Fatalf("unexpected successful update: %v", updated)
			}
			_, err := os.Stat(generated)
			keep := tc.shared || tc.source || tc.updateError || tc.lookupError
			if keep && err != nil {
				t.Fatalf("protected art removed: %v", err)
			}
			if !keep && !os.IsNotExist(err) {
				t.Fatalf("unused art left behind: %v", err)
			}
			if data, err := os.ReadFile(cover); err != nil || string(data) != "keep content" {
				t.Fatalf("shared sidecar changed: %q, %v", data, err)
			}
		})
	}
}
