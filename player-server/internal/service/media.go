package service

import (
	"context"
	"sync"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/probe"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/thumb"
)

var (
	_ MediaBrowseService   = (*mediaService)(nil)
	_ MediaWriteService    = (*mediaService)(nil)
	_ MediaShareService    = (*mediaService)(nil)
	_ MediaTagService      = (*mediaService)(nil)
	_ MediaFavoriteService = (*mediaService)(nil)
	_ MediaNoteService     = (*mediaService)(nil)
	_ MediaBookmarkService = (*mediaService)(nil)
	_ MediaService         = (*mediaService)(nil)
)

// mediaService is the concrete implementation of MediaService.
// It composes role-focused sub-services to satisfy SRP.
type mediaService struct {
	MediaBrowseService
	MediaWriteService
	MediaShareService
	MediaTagService
	MediaFavoriteService
	MediaNoteService
	MediaBookmarkService
}

// WithLyricsProvider configures optional, explicit lyric lookup before serving.
func (s *mediaService) WithLyricsProvider(provider *ProviderLyrics) *mediaService {
	if bs, ok := s.MediaBrowseService.(*browseService); ok {
		bs.lyricsProvider = provider
	}
	return s
}

// ChangeLyrics performs an explicitly requested provider cache action.
func (s *mediaService) ChangeLyrics(ctx context.Context, mediaID, userID int64, action string) (*Lyrics, error) {
	bs, ok := s.MediaBrowseService.(*browseService)
	if !ok {
		return nil, ErrForbidden
	}
	media, err := bs.helper.verifyAccess(ctx, mediaID, userID)
	if err != nil {
		return nil, err
	}
	result := bs.loadLyrics(ctx, media, userID, action)
	if _, err := bs.helper.verifyAccess(ctx, mediaID, userID); err != nil {
		return nil, err
	}
	return result, nil
}

// WithTrashLock coordinates mutations with the garbage collector. Configure
// before serving requests; the lock belongs to this application's lifetime.
func (s *mediaService) WithTrashLock(lock *sync.Mutex) *mediaService {
	if ws, ok := s.MediaWriteService.(*writeService); ok && lock != nil {
		ws.trashLock = lock
	}
	return s
}

// WithThumbnailLayout makes the write side (upload, regenerate thumbnail)
// create media thumbnails where layout says, i.e. in the thumbnail cache
// when one is configured. Generated covers are discovered there as well.
// It must be called before the service is used
// and does nothing for a write service that is not the built-in one.
func (s *mediaService) WithThumbnailLayout(layout thumb.Layout) *mediaService {
	if ws, ok := s.MediaWriteService.(*writeService); ok {
		ws.WithThumbnailLayout(layout)
	}
	if bs, ok := s.MediaBrowseService.(*browseService); ok {
		bs.WithThumbnailLayout(layout)
	}
	return s
}

// WithPathRoots allows writes under every registered music library root.
func (s *mediaService) WithPathRoots(roots *PathRoots) *mediaService {
	if ws, ok := s.MediaWriteService.(*writeService); ok {
		ws.WithPathRoots(roots)
	}
	return s
}

// NewMediaService creates a concrete MediaService by wiring role-focused sub-services.
func NewMediaService(store repository.MediaServiceStore, clk clock.Clock, mediaRoot string, thumbGen thumb.Generator, prober probe.Prober) *mediaService {
	return NewMediaServiceWithPodcastBrowser(store, clk, mediaRoot, thumbGen, prober, nil)
}

// NewMediaServiceWithPodcastBrowser creates a MediaService with an optional
// PodcastBrowser and the default filesystem thumbnail resolver. For
// dependency-injected setups (e.g. tests that want to avoid touching disk
// for thumbnails) use NewMediaServiceWithDeps.
func NewMediaServiceWithPodcastBrowser(store repository.MediaServiceStore, clk clock.Clock, mediaRoot string, thumbGen thumb.Generator, prober probe.Prober, browser PodcastBrowser) *mediaService {
	return NewMediaServiceWithDeps(store, clk, mediaRoot, thumbGen, prober, browser, thumb.NewFSResolver())
}

// NewMediaServiceWithDeps creates a MediaService with all collaborators
// supplied explicitly, including a thumbnail Resolver. A nil resolver
// falls back to the default filesystem implementation.
func NewMediaServiceWithDeps(store repository.MediaServiceStore, clk clock.Clock, mediaRoot string, thumbGen thumb.Generator, prober probe.Prober, browser PodcastBrowser, thumbResolver thumb.Resolver) *mediaService {
	helper := &accessHelper{store: store}
	shares := NewShareService(store, clk, helper)
	shares.lyricsRoot = mediaRoot
	return &mediaService{
		MediaBrowseService:   NewBrowseServiceWithResolver(store, clk, mediaRoot, helper, browser, thumbResolver),
		MediaWriteService:    NewWriteService(store, clk, mediaRoot, thumbGen, prober, helper),
		MediaShareService:    shares,
		MediaTagService:      NewTagService(store, helper),
		MediaFavoriteService: NewFavService(store, helper),
		MediaNoteService:     NewNoteService(store, clk, helper),
		MediaBookmarkService: NewBookmarkService(store, clk, helper),
	}
}
