package service

import (
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/probe"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/thumb"
)

var (
	_ MediaBrowseService   = (*mediaService)(nil)
	_ MediaWriteService    = (*mediaService)(nil)
	_ MediaShareService    = (*mediaService)(nil)
	_ MediaTagService      = (*mediaService)(nil)
	_ MediaFavoriteService = (*mediaService)(nil)
	_ MediaNoteService     = (*mediaService)(nil)
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
	return &mediaService{
		MediaBrowseService:   NewBrowseServiceWithResolver(store, clk, mediaRoot, helper, browser, thumbResolver),
		MediaWriteService:    NewWriteService(store, clk, mediaRoot, thumbGen, prober, helper),
		MediaShareService:    NewShareService(store, clk, helper),
		MediaTagService:      NewTagService(store, helper),
		MediaFavoriteService: NewFavService(store, helper),
		MediaNoteService:     NewNoteService(store, clk, helper),
	}
}
