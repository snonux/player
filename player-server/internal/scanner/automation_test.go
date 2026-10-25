package scanner

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/probe"
)

type failedSourceStatFS struct {
	osFS
	source string
}

func (f failedSourceStatFS) Stat(path string) (os.FileInfo, error) {
	if path == f.source {
		return nil, errors.New("source became inaccessible")
	}
	return f.osFS.Stat(path)
}

func TestInventoryRetainsMissingAndManualPolicy(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{
		{rel: "kept.mp3"}, {rel: "missing.mp3", noSource: true}, {rel: "trashed.mp3", deleted: true},
	})
	s := f.scanner()
	f.write(f.abs("new.mp3"), "new source")
	ctx := context.Background()
	if err := s.ScanInventory(ctx, f.root, nil); err != nil {
		t.Fatal(err)
	}
	if f.row("missing.mp3").DeletedAt != nil || f.row("kept.mp3").DeletedAt != nil {
		t.Fatal("automatic inventory deleted a record")
	}
	if f.row("trashed.mp3").DeletedAt == nil {
		t.Fatal("automatic inventory restored intentional trash")
	}
	if f.row("new.mp3").ID == 0 {
		t.Fatal("new source was not indexed")
	}
	if err := s.Scan(ctx, f.root, nil); err != nil {
		t.Fatal(err)
	}
	if f.row("missing.mp3").DeletedAt == nil || f.row("kept.mp3").DeletedAt != nil {
		t.Fatal("manual orphan policy changed")
	}
}

func TestInventoryPreservesUnrelatedIdentityConflicts(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{{rel: "missing.mp3", noSource: true}})
	ctx := context.Background()
	row := f.row("missing.mp3")
	if err := f.store.ReplaceMusicIdentityConflicts(ctx, []model.MusicIdentityConflict{{MissingMediaID: row.ID, MissingPath: row.RelPath, CandidateMediaIDs: "[]", Reason: "keep this choice"}}); err != nil {
		t.Fatal(err)
	}
	before, err := f.store.ListMusicIdentityConflicts(ctx)
	if err != nil || len(before) != 1 {
		t.Fatalf("seed conflicts %v %v", before, err)
	}
	f.write(f.abs("new.mp3"), "unrelated")
	if err := f.scanner().ScanInventory(ctx, f.root, nil); err != nil {
		t.Fatal(err)
	}
	after, err := f.store.ListMusicIdentityConflicts(ctx)
	if err != nil || len(after) != 1 || after[0].ID != before[0].ID {
		t.Fatalf("inventory cleared/replaced unrelated choice %v %v", after, err)
	}
}

func TestIncompleteDiscoveryDoesNotReconcile(t *testing.T) {
	for _, failure := range []error{context.Canceled, errors.New("walk failed")} {
		for _, inventory := range []bool{false, true} {
			f := newMigrationFixture(t, []seededMedia{{rel: "missing.mp3", noSource: true}, {rel: "kept.mp3"}})
			s := f.scanner()
			s.fs = walkErrorFS{path: f.abs("kept.mp3"), err: failure}
			var p model.ScanProgress
			var err error
			if inventory {
				err = s.ScanInventory(context.Background(), f.root, &p)
			} else {
				err = s.Scan(context.Background(), f.root, &p)
			}
			if err == nil {
				t.Fatal("expected discovery failure")
			}
			if f.row("missing.mp3").DeletedAt != nil {
				t.Fatal("partial discovery reconciled missing row")
			}
		}
	}
}

func TestFailedSourceProcessingDoesNotReconcile(t *testing.T) {
	for _, inventory := range []bool{false, true} {
		f := newMigrationFixture(t, []seededMedia{{rel: "missing.mp3", noSource: true}, {rel: "kept.mp3"}})
		s := f.scanner()
		s.fs = failedSourceStatFS{source: f.abs("kept.mp3")}
		var err error
		if inventory {
			err = s.ScanInventory(context.Background(), f.root, nil)
		} else {
			err = s.Scan(context.Background(), f.root, nil)
		}
		if err == nil {
			t.Fatal("expected source-processing failure")
		}
		if f.row("missing.mp3").DeletedAt != nil {
			t.Fatal("failed processing reconciled unseen source")
		}
	}
}

func TestInventorySourceCheckpointRequiresStableProbe(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{{rel: "song.mp3"}})
	s := f.scanner()
	probes := 0
	s.prober = &probe.MockProber{ProbeFunc: func(context.Context, string) (*model.Metadata, error) {
		probes++
		if probes == 1 {
			f.write(f.abs("song.mp3"), "changed while probing")
		}
		return &model.Metadata{Music: &model.MusicMetadata{Title: "Song"}}, nil
	}}
	ctx := context.Background()
	if err := s.ScanInventory(ctx, f.root, nil); err != nil {
		t.Fatal(err)
	}
	if f.row("song.mp3").Music.SourceMTime != "" {
		t.Fatal("unstable source was checkpointed")
	}
	if err := s.ScanInventory(ctx, f.root, nil); err != nil {
		t.Fatal(err)
	}
	if f.row("song.mp3").Music.SourceMTime == "" {
		t.Fatal("stable source not checkpointed")
	}
	count := probes
	if err := s.ScanInventory(ctx, f.root, nil); err != nil {
		t.Fatal(err)
	}
	if probes != count {
		t.Fatal("unchanged source was probed")
	}
	if err := s.Scan(ctx, f.root, nil); err != nil {
		t.Fatal(err)
	}
	if probes == count {
		t.Fatal("manual repair skipped full probing")
	}
}

func TestSourceSignatureIgnoresGeneratedAndDetectsSources(t *testing.T) {
	f := newMigrationFixture(t, []seededMedia{{rel: "kept.mp3"}})
	cache := filepath.Join(f.setPath, "renditions")
	s := f.scanner().WithIgnoredPaths(cache)
	ctx := context.Background()
	before, err := s.SourceSignature(ctx, f.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".thumbnails/kept.mp3.jpg", ".cover.jpg", "._kept.mp3", "renditions/1.mp4", ".hidden/song.mp3"} {
		f.write(f.abs(rel), "generated")
	}
	after, err := s.SourceSignature(ctx, f.root)
	if err != nil || after != before {
		t.Fatalf("generated outputs retrigger watcher %v", err)
	}
	f.write(f.abs("kept.lrc"), "[00:01]lyrics")
	after, err = s.SourceSignature(ctx, f.root)
	if err != nil || after == before {
		t.Fatalf("sidecar change invisible %v", err)
	}
	before = after
	f.write(f.abs("kept.mp3"), "retagged different source")
	after, err = s.SourceSignature(ctx, f.root)
	if err != nil || after == before {
		t.Fatalf("source change invisible %v", err)
	}
	s.fs = walkErrorFS{path: f.setPath, err: fs.ErrPermission}
	if _, err := s.SourceSignature(ctx, f.root); err == nil {
		t.Fatal("unreadable root accepted as complete observation")
	}
}
