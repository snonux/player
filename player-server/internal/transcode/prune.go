package transcode

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// cached is one finished rendition considered for eviction.
type cached struct {
	name    string
	size    int64
	lastUse time.Time
	// recent marks a rendition this instance handed out within pruneGrace.
	recent bool
}

// prune implements Prune: it brings the cache back under Options.MaxBytes.
// extra is a number of bytes to free on top of that, used when the volume
// itself is short of space for a rendition that is about to be produced.
//
// Recently handed-out renditions are skipped even when that leaves the cache
// over its limit for a moment: evicting a file a request is just about to
// open would fail that playback, and the next prune catches up.
//
// The most recently used rendition is never evicted at all. Otherwise a
// single rendition larger than MaxBytes would be deleted at the next GC tick
// while it is being played, and every following Range request would start
// the transcode again. The cache can therefore exceed MaxBytes by that one
// file.
func (c *Cache) prune(ctx context.Context, extra int64) error {
	files, total, err := c.scan()
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, k int) bool { return files[i].lastUse.Before(files[k].lastUse) })

	limit := max(min(c.opts.MaxBytes, total-extra), 0)
	var errs []error
	for _, f := range files[:max(len(files)-1, 0)] {
		if total <= limit || ctx.Err() != nil {
			break
		}
		if f.recent {
			continue
		}
		if err := os.Remove(filepath.Join(c.opts.Dir, f.name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		total -= f.size
		c.logger.Info("transcode cache evicted", "rendition", f.name, "size", f.size)
	}
	return errors.Join(errs...)
}

// scan lists finished renditions and the total bytes the cache occupies.
//
// Temporary files of running jobs count towards the total (they are real
// disk usage) but are not evictable. A temporary file nobody has written to
// for tmpMaxAge is a leftover of a crashed or killed process and is deleted;
// age rather than "is it one of my jobs" decides, because another instance
// may share the directory during a rolling update.
func (c *Cache) scan() ([]cached, int64, error) {
	entries, err := c.readDir()
	if err != nil {
		return nil, 0, err
	}
	now := c.clk.Now()
	uses := c.lastUseSnapshot(entries, now)

	var files []cached
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		switch name := e.Name(); {
		case tmpRe.MatchString(name):
			if now.Sub(info.ModTime()) > tmpMaxAge {
				_ = os.Remove(filepath.Join(c.opts.Dir, name))
				continue
			}
			total += info.Size()
		case renditionRe.MatchString(name):
			f := cached{name: name, size: info.Size(), lastUse: info.ModTime()}
			if used, ok := uses[name]; ok {
				f.lastUse, f.recent = used, now.Sub(used) < pruneGrace
			}
			files = append(files, f)
			total += info.Size()
		}
	}
	return files, total, nil
}

// lastUseSnapshot copies the LRU clock and forgets entries whose file is
// gone (evicted, removed, or deleted by another instance). Entries younger
// than pruneGrace are kept regardless, because their file may have been
// published after the directory listing was taken.
func (c *Cache) lastUseSnapshot(entries []fs.DirEntry, now time.Time) map[string]time.Time {
	onDisk := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		onDisk[e.Name()] = struct{}{}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	uses := make(map[string]time.Time, len(c.lastUse))
	for name, used := range c.lastUse {
		if _, ok := onDisk[name]; !ok && now.Sub(used) >= pruneGrace {
			delete(c.lastUse, name)
			continue
		}
		uses[name] = used
	}
	return uses
}
