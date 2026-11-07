package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// PodcastEpisodeStreamer opens a publisher stream without importing a server
// copy. Callers own Body and must close it, including on client disconnect.
type PodcastEpisodeStreamer interface {
	OpenEpisodeStream(context.Context, int64, int64, string, string, string) (*http.Response, error)
}

func (s *podcastService) OpenEpisodeStream(ctx context.Context, episodeID, userID int64, method, byteRange, ifRange string) (*http.Response, error) {
	episode, err := s.store.GetEpisodeByID(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	if episode == nil {
		return nil, ErrNotFound
	}
	feed, err := s.store.GetFeedByID(ctx, episode.FeedID)
	if err != nil {
		return nil, err
	}
	if feed == nil {
		return nil, ErrNotFound
	}
	// A subscription permits streaming its publisher; server-copy permissions
	// remain separate. Set browsing also permits playback without a subscription.
	if err := s.helper.checkSetPermission(ctx, feed.SetID, userID, ""); err != nil {
		if !errors.Is(err, ErrForbidden) {
			return nil, err
		}
		subscribed, lookupErr := s.store.GetSubscribedPodcastFeed(ctx, userID, feed.FeedKey)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if subscribed == nil {
			return nil, err
		}
	}
	target, err := url.Parse(episode.EpisodeURL)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.User != nil {
		return nil, ErrInvalidFeed
	}
	if method != http.MethodGet && method != http.MethodHead {
		return nil, ErrInvalidFeed
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	if ifRange != "" {
		req.Header.Set("If-Range", ifRange)
	}
	// The selected client enforces the same per-feed outbound address policy as
	// downloads. Never forward session cookies, bearer tokens or other headers.
	client := *s.enclosureClient(ctx, feed)
	client.Jar = nil
	client.Timeout = 0 // A listening stream may outlive a short download timeout.
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.User != nil {
			return ErrInvalidFeed
		}
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		req.Header.Del("Referer")
		if err := httpRedirectsOnly(req, via); err != nil {
			return err
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open podcast stream: %w", err)
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent && response.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		response.Body.Close()
		return nil, fmt.Errorf("publisher returned status %d", response.StatusCode)
	}
	mime := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if response.StatusCode != http.StatusRequestedRangeNotSatisfiable && mime != "" && !strings.HasPrefix(mime, "audio/") && !strings.HasPrefix(mime, "video/") && mime != "application/octet-stream" && mime != "application/ogg" {
		response.Body.Close()
		return nil, fmt.Errorf("publisher did not return media")
	}
	return response, nil
}
