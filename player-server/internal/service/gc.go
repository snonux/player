package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

// RenditionGC is the part of the transcode cache the GC worker maintains.
// It is implemented by transcode.Cache.
type RenditionGC interface {
	// Prune enforces the cache size bound and removes abandoned files.
	Prune(ctx context.Context) error
	// Remove deletes all renditions of a media item.
	Remove(mediaID int64) error
}

// GCWorker is a background worker that hard-deletes soft-deleted media older
// than a threshold and, when a rendition cache is attached, keeps that cache
// bounded on the same tick.
type GCWorker struct {
	trashLock   *sync.Mutex
	lifecycleMu sync.Mutex
	store       repository.GCStore
	clock       clock.Clock
	interval    time.Duration
	age         time.Duration
	logger      *slog.Logger
	ticker      *time.Ticker
	tickCh      <-chan time.Time
	runDoneCh   chan struct{}
	stopCh      chan struct{}
	startOnce   sync.Once
	stopOnce    sync.Once
	wg          sync.WaitGroup
	mediaRoot   string
	// thumbRm deletes the generated thumbnail of a purged media item,
	// which thumbLayout locates (see WithThumbnailLayout).
	thumbRm     thumbnailRemover
	thumbLayout thumb.Layout
	// renditions is optional; nil disables transcode cache maintenance.
	renditions RenditionGC
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewGCWorker creates a GCWorker. Use WithAge and WithInterval to customise.
func NewGCWorker(store repository.GCStore, clk clock.Clock, mediaRoot string, interval time.Duration, logger *slog.Logger) *GCWorker {
	return &GCWorker{
		store:     store,
		trashLock: new(sync.Mutex),
		clock:     clk,
		interval:  interval,
		age:       7 * 24 * time.Hour,
		logger:    logger,
		stopCh:    make(chan struct{}),
		mediaRoot: mediaRoot,
		thumbRm:   newThumbnailRemover(logger, thumb.Layout{}),
	}
}

// WithTrashLock coordinates expiry with soft-delete/restore in this process.
// Set it before Start; production wiring shares one lock with the write service.
func (w *GCWorker) WithTrashLock(lock *sync.Mutex) *GCWorker {
	if lock != nil {
		w.trashLock = lock
	}
	return w
}

// WithThumbnailLayout tells the worker where generated thumbnails live, so
// a purged item's thumbnail is also found in the thumbnail cache. Without
// it thumbnails are expected beside their source.
func (w *GCWorker) WithThumbnailLayout(layout thumb.Layout) *GCWorker {
	w.thumbLayout = layout
	w.thumbRm = newThumbnailRemover(w.logger, layout)
	return w
}

// WithAge overrides the default 7-day deletion age.
func (w *GCWorker) WithAge(age time.Duration) *GCWorker {
	w.age = age
	return w
}

// WithInterval overrides the ticker interval (used in tests that need deterministic ticks).
func (w *GCWorker) WithInterval(interval time.Duration) *GCWorker {
	w.interval = interval
	return w
}

// WithRenditionCache attaches the transcode cache so every GC run prunes it
// and renditions of purged media are removed with their source.
func (w *GCWorker) WithRenditionCache(renditions RenditionGC) *GCWorker {
	w.renditions = renditions
	return w
}

// Start launches the GC goroutine. Idempotent: subsequent calls after the
// first are no-ops. Paired with Stop (also one-shot via sync.Once); the
// worker is not designed to be restarted after Stop, so a sync.Once guard
// matches the existing lifetime semantics and prevents goroutine leaks
// from accidental double-Start.
func (w *GCWorker) Start() {
	w.startOnce.Do(func() {
		w.lifecycleMu.Lock()
		select {
		case <-w.stopCh:
			w.lifecycleMu.Unlock()
			return
		default:
		}
		w.ctx, w.cancel = context.WithCancel(context.Background())
		tickCh := w.tickCh
		if tickCh == nil {
			w.ticker = time.NewTicker(w.interval)
			tickCh = w.ticker.C
		}
		// Count startup work before Stop can begin waiting. The goroutine owns
		// this count once startup completes, even if shutdown already began.
		w.wg.Add(1)
		w.lifecycleMu.Unlock()
		// Finish the startup expiry pass before the server accepts requests.
		w.runSafely(w.ctx)
		go func() {
			defer w.wg.Done()
			for {
				select {
				case <-tickCh:
					w.runSafely(w.ctx)
					w.notifyRunDone()
				case <-w.stopCh:
					return
				}
			}
		}()
	})
}

// Stop stops the GC goroutine and waits for it to finish.
// Safe to call multiple times or before Start() (idempotent, no-op).
func (w *GCWorker) Stop() {
	w.stopOnce.Do(func() {
		w.lifecycleMu.Lock()
		defer w.lifecycleMu.Unlock()
		if w.ticker != nil {
			w.ticker.Stop()
		}
		if w.cancel != nil {
			w.cancel()
		}
		close(w.stopCh)
	})
	w.wg.Wait()
}

func (w *GCWorker) runSafely(ctx context.Context) {
	defer func() { RecoverWorker(w.logger, "gc", recover()) }()
	w.run(ctx)
}

// run performs one GC pass: purge expired trash, then prune the transcode
// cache. The cache is pruned even when listing the trash fails, because the
// two concerns are independent.
func (w *GCWorker) run(ctx context.Context) {
	w.purgeExpired(ctx)
	w.pruneRenditions(ctx)
}

// purgeExpired purges every soft-deleted media item older than the
// configured age.
func (w *GCWorker) purgeExpired(ctx context.Context) {
	// Read the current deletion state under the same lock as restore. Holding
	// it through this batch prevents a stale snapshot from deleting a restored
	// source, and keeps the lookup linear in the size of the trash.
	w.trashLock.Lock()
	defer w.trashLock.Unlock()
	if ctx.Err() != nil {
		return
	}
	items, err := w.store.ListDeletedMedia(ctx)
	if err != nil {
		if w.logger != nil {
			w.logger.Error("gc list deleted media", "err", err)
		}
		return
	}

	cutoff := w.clock.Now().Add(-w.age)
	for i := range items {
		if ctx.Err() != nil {
			return
		}
		if items[i].DeletedAt == nil || items[i].DeletedAt.After(cutoff) {
			continue
		}
		w.purge(ctx, &items[i])
	}
}

// purge deletes one media item for good: its file, then its row, then the
// files derived from it — the generated thumbnail and the cached transcode
// renditions. The file goes before the row so a failure never leaves a file
// on disk that no row accounts for; if the file cannot be removed the item
// is kept and retried on the next run, except permission/read-only errors:
// these purge only the row, since retrying cannot remove a read-only source.
// A subsequent rescan can rediscover that unchanged file. The derived files go last, once
// nothing refers to them any more; a thumbnail left in place would be an
// orphan forever, and a rendition would keep deleted content playable from
// the cache until it happened to be evicted.
func (w *GCWorker) purge(ctx context.Context, item *model.Media) {
	absPath := item.AbsPath
	if absPath == "" {
		if item.RelPath == "" {
			if w.logger != nil {
				w.logger.Warn("gc source path missing", "id", item.ID)
			}
			return
		}
		set, err := w.store.GetSetByID(ctx, item.SetID)
		if err != nil || set == nil || set.RootPath == "" {
			if w.logger != nil {
				w.logger.Warn("gc cannot resolve source set", "id", item.ID, "set_id", item.SetID, "err", err)
			}
			return
		}
		absPath = filepath.Clean(filepath.Join(w.mediaRoot, set.RootPath, filepath.FromSlash(item.RelPath)))
	}

	if !confinedPath(w.mediaRoot, absPath) {
		// An inaccessible ancestor is different from an escaping symlink.
		// Neither may be unlinked safely, but legacy inaccessible trash can
		// still lose its row once without touching any filesystem paths.
		if _, err := os.Lstat(absPath); lexicalPathWithin(w.mediaRoot, absPath) && readonlyError(err) {
			w.purgeInaccessible(ctx, item)
			return
		}
		if w.logger != nil {
			w.logger.Warn("gc refusing source outside media root", "id", item.ID, "path", absPath)
		}
		return
	}

	// Thumbnail ownership needs the resolved source even for legacy rows.
	resolvedItem := *item
	resolvedItem.AbsPath = absPath
	item = &resolvedItem

	// .cover.jpg is generated artwork shared by the set/folder, not media
	// owned by this legacy row. Keep it when collecting a row imported by
	// an older scanner (including rows already soft-deleted before the fix).
	if absPath != "" && filepath.Base(filepath.FromSlash(item.RelPath)) != ".cover.jpg" {
		// A file that is already gone is fine; proceed with the row.
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			if readonlyError(err) {
				// Legacy trash in a read-only set cannot be removed. Purge its
				// row once; a later rescan may import the unchanged file again.
				if w.logger != nil {
					w.logger.Warn("gc keeping read-only source, purging row", "id", item.ID, "path", absPath)
				}
			} else {
				if w.logger != nil {
					w.logger.Warn("gc remove file", "path", absPath, "err", err)
				}
				return
			}
		}
	}

	if err := w.store.HardDeleteMedia(ctx, item.ID); err != nil {
		if w.logger != nil {
			w.logger.Error("gc hard delete", "id", item.ID, "err", err)
		}
		return
	}
	if confinedPath(w.mediaRoot, item.ThumbnailPath) || (w.thumbLayout.CacheDir() != "" && confinedPath(w.thumbLayout.CacheDir(), item.ThumbnailPath)) {
		removeOwnThumbnail(w.thumbRm, w.thumbLayout, item)
	}
	w.removeRenditions(item.ID)

	if w.logger != nil {
		w.logger.Info("gc deleted media", "id", item.ID, "path", absPath)
	}
}

// purgeInaccessible removes only database/cache state when source confinement
// cannot be resolved because a directory refuses access. Thumbnail paths may
// use the same inaccessible ancestors and are deliberately left untouched.
func (w *GCWorker) purgeInaccessible(ctx context.Context, item *model.Media) {
	if err := w.store.HardDeleteMedia(ctx, item.ID); err != nil {
		if w.logger != nil {
			w.logger.Error("gc hard delete", "id", item.ID, "err", err)
		}
		return
	}
	w.removeRenditions(item.ID)
	if w.logger != nil {
		w.logger.Warn("gc keeping inaccessible source, purging row", "id", item.ID)
	}
}

// removeRenditions drops the cached renditions of a purged item (and stops a
// transcode of it that is still running).
func (w *GCWorker) removeRenditions(mediaID int64) {
	if w.renditions == nil {
		return
	}
	if err := w.renditions.Remove(mediaID); err != nil && w.logger != nil {
		w.logger.Warn("gc remove renditions", "id", mediaID, "err", err)
	}
}

// pruneRenditions enforces the transcode cache size bound.
func (w *GCWorker) pruneRenditions(ctx context.Context) {
	if w.renditions == nil {
		return
	}
	if err := w.renditions.Prune(ctx); err != nil && w.logger != nil {
		w.logger.Warn("gc prune transcode cache", "err", err)
	}
}

func (w *GCWorker) notifyRunDone() {
	if w.runDoneCh == nil {
		return
	}
	select {
	case w.runDoneCh <- struct{}{}:
	default:
	}
}

// RunOnce performs a single GC run synchronously. Useful for tests.
func (w *GCWorker) RunOnce() error {
	if w.interval == 0 {
		return errors.New("worker not started")
	}
	w.lifecycleMu.Lock()
	ctx := w.ctx
	w.lifecycleMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	w.run(ctx)
	return nil
}
