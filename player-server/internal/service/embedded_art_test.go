package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/ffsafe/ffsafetest"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/probe"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/scanner"
	"github.com/snonux/player/internal/thumb"
)

func TestAudioArtwork_RealImportAndRegenerate(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	ctx := context.Background()
	root, cache := t.TempDir(), t.TempDir()
	track := filepath.Join(root, "Music", "track.flac")
	plain := filepath.Join(root, "Music", "plain.flac")
	ffsafetest.Audio(t, track, true)
	ffsafetest.Audio(t, plain, false)
	store, err := repository.Open(filepath.Join(t.TempDir(), "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	setID, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	adminID, err := store.CreateUser(ctx, &model.User{Username: "admin", IsAdmin: true, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	viewerID, err := store.CreateUser(ctx, &model.User{Username: "outsider", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	layout := thumb.NewLayout(root, cache)
	prober, generator := probe.NewFFProber(), thumb.NewFFmpegGenerator()
	maker := thumb.NewFSMaker(generator, nil, nil).WithLayout(layout)
	rows := make([]*model.Media, 0, 2)
	for _, path := range []string{track, plain} {
		m := &model.Media{SetID: setID, RelPath: filepath.Base(path), FileName: filepath.Base(path), AbsPath: path, Type: model.MediaTypeAudio, CreatedAt: time.Now()}
		m.ID, err = store.CreateMedia(ctx, m)
		if err != nil {
			t.Fatal(err)
		}
		if err := ImportMediaFile(ctx, store, m, prober, maker); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, m)
	}
	want, _ := layout.PathFor(track)
	if rows[0].ThumbnailPath != want || rows[1].ThumbnailPath != "" {
		t.Fatalf("import paths: %q and %q", rows[0].ThumbnailPath, rows[1].ThumbnailPath)
	}
	if info, err := os.Stat(want); err != nil || info.Size() == 0 {
		t.Fatalf("imported artwork missing: %v", err)
	}
	svc := NewWriteService(store, clock.RealClock{}, root, generator, prober, NewAccessHelper(store)).WithThumbnailLayout(layout)
	if err := svc.RegenerateThumbnail(ctx, rows[0].ID, viewerID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unauthorized regeneration: %v", err)
	}
	if err := os.Remove(want); err != nil {
		t.Fatal(err)
	}
	if err := svc.RegenerateThumbnail(ctx, rows[0].ID, adminID); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(want); err != nil || info.Size() == 0 {
		t.Fatalf("regenerated artwork missing: %v", err)
	}
	if err := svc.RegenerateThumbnail(ctx, rows[1].ID, adminID); err == nil {
		t.Fatal("regenerated nonexistent embedded art")
	}
	row, err := store.GetMediaByID(ctx, rows[1].ID)
	if err != nil || row.ThumbnailPath != "" {
		t.Fatalf("no-art row changed: %+v, %v", row, err)
	}
}

func TestAudioArtworkImportPreservesSharedArtAndCleansFailedWrite(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	src := filepath.Join(root, "track.flac")
	shared := filepath.Join(root, "cover.jpg")
	if err := os.WriteFile(shared, []byte("shared artwork"), 0o644); err != nil {
		t.Fatal(err)
	}
	index := 1
	prober := &probe.MockProber{ProbeFunc: func(context.Context, string) (*model.Metadata, error) {
		return &model.Metadata{EmbeddedArtIndex: &index}, nil
	}}
	maker := newThumbnailMaker(writingThumbGen(), nil)
	m := &model.Media{Type: model.MediaTypeAudio, AbsPath: src, ThumbnailPath: shared}
	if err := ImportMediaFile(ctx, &updateRecorder{}, m, prober, maker); err != nil {
		t.Fatal(err)
	}
	if m.ThumbnailPath != shared {
		t.Fatalf("replaced shared art with %q", m.ThumbnailPath)
	}
	m.ThumbnailPath = ""
	if err := ImportMediaFile(ctx, &updateRecorder{err: errors.New("write failed")}, m, prober, maker); err == nil {
		t.Fatal("lost database error")
	}
	if _, err := os.Stat(thumb.ThumbnailPathFor(src)); !os.IsNotExist(err) {
		t.Fatalf("failed import left artwork: %v", err)
	}
	if content, err := os.ReadFile(shared); err != nil || string(content) != "shared artwork" {
		t.Fatalf("shared art changed: %q, %v", content, err)
	}
}

// artworkAdoptionStore pauses a scan after its reference snapshot and before
// switching the row, making concurrent regeneration contend at that boundary.
type artworkAdoptionStore struct {
	repository.Store
	adopting chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (s *artworkAdoptionStore) UpdateMediaThumbnail(ctx context.Context, id int64, path string) error {
	s.once.Do(func() {
		close(s.adopting)
		select {
		case <-s.release:
		case <-ctx.Done():
		}
	})
	return s.Store.UpdateMediaThumbnail(ctx, id, path)
}

func TestAudioArtwork_RealConcurrentAdoptionAndRegeneration(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root, cache := t.TempDir(), t.TempDir()
	track := filepath.Join(root, "Music", "track.flac")
	cover := filepath.Join(root, "Music", "cover.jpg")
	ffsafetest.Audio(t, track, true)
	ffsafetest.Still(t, cover)
	base, err := repository.Open(filepath.Join(t.TempDir(), "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	setID, err := base.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	adminID, err := base.CreateUser(ctx, &model.User{Username: "admin", IsAdmin: true, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	layout := thumb.NewLayout(root, cache)
	generated, _ := layout.PathFor(track)
	gen, prober := thumb.NewFFmpegGenerator(), probe.NewFFProber()
	maker := thumb.NewFSMaker(gen, nil, nil).WithLayout(layout)
	if _, err := maker.Make(ctx, track, 0); err != nil {
		t.Fatal(err)
	}
	trackID, err := base.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: "track.flac", FileName: "track.flac", AbsPath: track, Type: model.MediaTypeAudio, ThumbnailPath: generated, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	store := &artworkAdoptionStore{Store: base, adopting: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(store.release) }) }
	defer release()
	lock := new(sync.Mutex)
	scan := scanner.NewFSScannerWithMaker(store, prober, maker, clock.RealClock{}, root, nil).
		WithThumbnailLayout(layout).WithTrashLock(lock)
	regenGen := &thumb.MockGenerator{GenerateFunc: func(ctx context.Context, input, output string, duration float64) error {
		// Once the scanner releases the barrier, regeneration must own the
		// same lock throughout writing and selecting its new thumbnail.
		if lock.TryLock() {
			lock.Unlock()
			t.Error("regeneration wrote artwork without the shared mutation lock")
		}
		return gen.Generate(ctx, input, output, duration)
	}}
	svc := NewMediaService(store, clock.RealClock{}, root, regenGen, prober).
		WithThumbnailLayout(layout).WithTrashLock(lock)
	var operations sync.WaitGroup
	defer func() {
		release()
		cancel()
		operations.Wait()
	}()
	scanned := make(chan error, 1)
	operations.Add(1)
	go func() {
		defer operations.Done()
		scanned <- scan.Scan(ctx, root, nil)
	}()
	select {
	case <-store.adopting:
	case <-ctx.Done():
		t.Fatal("scan did not reach artwork adoption")
	}
	// This verifies the scan's critical section without a timing assumption.
	if lock.TryLock() {
		lock.Unlock()
		t.Error("scan switched artwork without the shared mutation lock")
	}
	started := make(chan struct{})
	regenerated := make(chan error, 1)
	operations.Add(1)
	go func() {
		defer operations.Done()
		close(started)
		regenerated <- svc.RegenerateThumbnail(ctx, trackID, adminID)
	}()
	<-started
	release()
	for _, done := range []chan error{scanned, regenerated} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("concurrent artwork operations did not finish")
		}
	}
	row, err := base.GetMediaByID(ctx, trackID)
	if err != nil || row == nil || row.ThumbnailPath != generated {
		t.Fatalf("regenerated row = %+v, %v", row, err)
	}
	if info, err := os.Stat(row.ThumbnailPath); err != nil || info.Size() == 0 {
		t.Fatalf("concurrent scan deleted the selected artwork: %v", err)
	}
	if _, err := os.Stat(cover); err != nil {
		t.Fatalf("shared artwork was deleted: %v", err)
	}
	// Repeat only after checking the concurrent result, so this cannot heal
	// a missing file and conceal a race. With the scanner gone, the generator's
	// TryLock assertion checks regeneration's own critical section independently.
	if err := svc.RegenerateThumbnail(ctx, trackID, adminID); err != nil {
		t.Fatal(err)
	}
}
