package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/probe"
	"github.com/snonux/player/internal/repository"
)

type interruptedInsertStore struct {
	*repository.SQLite
	cancel context.CancelFunc
}

func (s *interruptedInsertStore) CreateMedia(ctx context.Context, row *model.Media) (int64, error) {
	id, err := s.SQLite.CreateMedia(ctx, row)
	if err == nil && row.FileName == "renamed.mp3" {
		s.cancel()
	}
	return id, err
}

func TestInventoryInterruptedRenameRetry(t *testing.T) {
	for _, boundary := range []string{"cancel after insert", "later set fails"} {
		for _, destinationUsed := range []bool{false, true} {
			t.Run(boundary+"/destinationUsed="+map[bool]string{false: "false", true: "true"}[destinationUsed], func(t *testing.T) {
				f := newMigrationFixture(t, []seededMedia{{rel: "original.mp3"}, {rel: "unrelated-stable.mp3"}})
				makeScanner := func() *FSScanner {
					s := f.scanner()
					s.prober = &probe.MockProber{ProbeFunc: func(context.Context, string) (*model.Metadata, error) {
						return &model.Metadata{Music: &model.MusicMetadata{Title: "Same song", Artist: "Artist", Album: "Album"}}, nil
					}}
					return s
				}
				ctx := context.Background()
				if err := makeScanner().ScanInventory(ctx, f.root, nil); err != nil {
					t.Fatal(err)
				}
				original := f.row("original.mp3")
				unrelated := f.row("unrelated-stable.mp3")
				user, err := f.store.CreateUser(ctx, &model.User{Username: "listener", PasswordHash: "hash", CreatedAt: time.Now()})
				if err != nil {
					t.Fatal(err)
				}
				if err := f.store.UpsertProgress(ctx, &model.PlaybackProgress{UserID: user, MediaID: original.ID, PositionSeconds: 12, UpdatedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.ToggleFavorite(ctx, user, original.ID); err != nil {
					t.Fatal(err)
				}
				if err := f.store.IncrementPlayCount(ctx, original.ID); err != nil {
					t.Fatal(err)
				}
				if err := f.store.CreatePlaylist(ctx, user, model.Playlist{ID: "keep", Name: "Keep"}); err != nil {
					t.Fatal(err)
				}
				if ok, err := f.store.AppendPlaylist(ctx, user, "keep", []model.PlaylistEntry{{ID: "occurrence", MediaID: original.ID}}); err != nil || !ok {
					t.Fatalf("append %v %v", ok, err)
				}
				if err := os.Rename(f.abs("original.mp3"), f.abs("renamed.mp3")); err != nil {
					t.Fatal(err)
				}
				first := makeScanner()
				runCtx, cancel := context.WithCancel(ctx)
				defer cancel()
				if boundary == "cancel after insert" {
					first.store = &interruptedInsertStore{SQLite: f.store.SQLite, cancel: cancel}
				} else {
					bad := filepath.Join(f.root, "zz-later", "bad.mp3")
					f.write(bad, "source")
					first.fs = walkErrorFS{path: bad, err: errors.New("later discovery failed")}
				}
				if err := first.ScanInventory(runCtx, f.root, nil); err == nil {
					t.Fatal("expected interrupted inventory")
				}
				destination := f.row("renamed.mp3")
				if destinationUsed {
					if err := f.store.UpsertProgress(ctx, &model.PlaybackProgress{UserID: user, MediaID: destination.ID, PositionSeconds: 7, UpdatedAt: time.Now()}); err != nil {
						t.Fatal(err)
					}
				}
				if boundary == "later set fails" {
					if err := os.RemoveAll(filepath.Join(f.root, "zz-later")); err != nil {
						t.Fatal(err)
					}
				}
				// New scanner instance proves retry provenance survived in SQLite.
				if err := makeScanner().ScanInventory(ctx, f.root, nil); err != nil {
					t.Fatal(err)
				}
				rows, err := f.store.ListMedia(ctx, repository.MediaFilter{})
				if err != nil {
					t.Fatal(err)
				}
				if !destinationUsed {
					kept, err := f.store.GetMediaByID(ctx, original.ID)
					if err != nil || len(rows) != 2 || kept == nil || kept.FileName != "renamed.mp3" {
						t.Fatalf("retry lost original identity: %+v", rows)
					}
				} else {
					if len(rows) != 3 {
						t.Fatalf("used destination was deleted: %+v", rows)
					}
					p, err := f.store.GetProgress(ctx, user, destination.ID)
					if err != nil || p == nil || p.PositionSeconds != 7 {
						t.Fatalf("destination state lost %+v %v", p, err)
					}
					conflicts, err := f.store.ListMusicIdentityConflicts(ctx)
					if err != nil || len(conflicts) != 1 {
						t.Fatalf("unsafe merge lacks conflict %+v %v", conflicts, err)
					}
				}
				stable, err := f.store.GetMediaByID(ctx, unrelated.ID)
				if err != nil || stable == nil || stable.RelPath != "unrelated-stable.mp3" {
					t.Fatalf("unrelated stable row treated as candidate %+v %v", stable, err)
				}
				kept, err := f.store.GetMediaByID(ctx, original.ID)
				if err != nil || kept == nil || kept.PlayCount != 1 {
					t.Fatalf("count lost %+v %v", kept, err)
				}
				p, err := f.store.GetProgress(ctx, user, original.ID)
				if err != nil || p == nil || p.PositionSeconds != 12 {
					t.Fatalf("resume lost %+v %v", p, err)
				}
				favorites, err := f.store.ListFavoritesByUser(ctx, user)
				if err != nil || len(favorites) != 1 || favorites[0].MediaID != original.ID {
					t.Fatalf("favorite lost %+v %v", favorites, err)
				}
				playlist, err := f.store.GetPlaylist(ctx, user, "keep")
				if err != nil || len(playlist.Entries) != 1 || playlist.Entries[0].ID != "occurrence" || playlist.Entries[0].MediaID != original.ID {
					t.Fatalf("occurrence lost %+v %v", playlist, err)
				}
			})
		}
	}
}

func TestSourceSignatureConfiguredSymlinkRoot(t *testing.T) {
	for _, registered := range []bool{false, true} {
		t.Run(map[bool]string{false: "MEDIA_ROOT", true: "registered"}[registered], func(t *testing.T) {
			f := newMigrationFixture(t, nil)
			link := filepath.Join(t.TempDir(), "trusted-root")
			if err := os.Symlink(f.root, link); err != nil {
				t.Fatal(err)
			}
			root := link
			if registered {
				if _, err := f.store.EnsureDefaultMusicLibrary(context.Background(), link, time.Now()); err != nil {
					t.Fatal(err)
				}
				root = t.TempDir()
			}
			s := f.scanner()
			s.WithIgnoredPaths(filepath.Join(link, "generated"))
			title := "Added"
			s.prober = &probe.MockProber{ProbeFunc: func(context.Context, string) (*model.Metadata, error) {
				return &model.Metadata{Music: &model.MusicMetadata{Title: title}}, nil
			}}
			before, err := s.SourceSignature(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			f.write(f.abs("new.mp3"), "new")
			after, err := s.SourceSignature(context.Background(), root)
			if err != nil || after == before {
				t.Fatalf("symlink addition invisible %v", err)
			}
			if err := s.ScanInventory(context.Background(), root, nil); err != nil {
				t.Fatal(err)
			}
			if f.row("new.mp3").Music.Title != "Added" {
				t.Fatal("symlink addition was not indexed")
			}
			title = "Retagged"
			f.write(f.abs("new.mp3"), "retagged source")
			retagged, err := s.SourceSignature(context.Background(), root)
			if err != nil || retagged == after {
				t.Fatalf("symlink retag invisible %v", err)
			}
			if err := s.ScanInventory(context.Background(), root, nil); err != nil {
				t.Fatal(err)
			}
			if f.row("new.mp3").Music.Title != "Retagged" {
				t.Fatal("symlink retag did not update catalog")
			}
			// Resolving the configured root must not follow nested symlinks or
			// stop excluding a configured cache addressed through that alias.
			outside := t.TempDir()
			if err := os.Symlink(outside, f.abs("nested-link")); err != nil {
				t.Fatal(err)
			}
			f.write(filepath.Join(outside, "outside.mp3"), "external")
			f.write(filepath.Join(link, "generated", "cache.mp3"), "generated")
			ignored, err := s.SourceSignature(context.Background(), root)
			if err != nil || ignored != retagged {
				t.Fatalf("nested link/cache retriggered observation %v", err)
			}
		})
	}
}
