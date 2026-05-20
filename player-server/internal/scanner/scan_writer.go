// Package scanner implements media library scanning logic.
package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync/atomic"

	"codeberg.org/snonux/player/internal/repository"
)

// scanWriter persists probed media results to the database. It runs in a
// single goroutine to serialise SQLite writes and avoid concurrent-write
// errors. All probing and thumbnail generation happens in probeWorker before
// results arrive here.
type scanWriter struct {
	store  repository.ScannerStore
	logger *slog.Logger
}

// newScanWriter creates a scanWriter backed by the given store.
func newScanWriter(store repository.ScannerStore, logger *slog.Logger) *scanWriter {
	return &scanWriter{store: store, logger: logger}
}

// run reads fileResults from resultChan and inserts each into the store.
// It logs progress every 25 files and accumulates the count in newFiles.
// Exits when resultChan is closed or scanCtx is cancelled.
func (sw *scanWriter) run(
	ctx context.Context,
	scanCtx context.Context,
	resultChan <-chan fileResult,
	setName string,
	setPath string,
	newFiles *int32,
	sendErr func(error),
) {
	for result := range resultChan {
		if scanCtx.Err() != nil {
			continue
		}
		if _, err := sw.store.CreateMedia(ctx, result.media); err != nil {
			sendErr(fmt.Errorf("create media %q: %w", result.path, err))
			continue
		}
		nf := atomic.AddInt32(newFiles, 1)
		// Log progress on the first insert and every 25 thereafter to give
		// operators visibility into long-running scans without flooding logs.
		if nf == 1 || nf%25 == 0 {
			relPath, _ := filepath.Rel(setPath, result.path)
			sw.logger.Info("scanner set progress", "name", setName, "new_media", nf, "latest", filepath.ToSlash(relPath))
		}
	}
}
