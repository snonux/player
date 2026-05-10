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

// NewMediaServiceWithPodcastBrowser creates a MediaService with an optional PodcastBrowser.
func NewMediaServiceWithPodcastBrowser(store repository.MediaServiceStore, clk clock.Clock, mediaRoot string, thumbGen thumb.Generator, prober probe.Prober, browser PodcastBrowser) *mediaService {
	helper := &accessHelper{store: store}
	return &mediaService{
		MediaBrowseService:   NewBrowseService(store, clk, mediaRoot, helper, browser),
		MediaWriteService:    NewWriteService(store, clk, mediaRoot, thumbGen, prober, helper),
		MediaShareService:    NewShareService(store, clk, helper),
		MediaTagService:      NewTagService(store, helper),
		MediaFavoriteService: NewFavService(store, helper),
		MediaNoteService:     NewNoteService(store, clk, helper),
	}
}
