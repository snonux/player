package service

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/podcast"
	"codeberg.org/snonux/player/internal/repository"
)

// Limits of POST /api/v1/podcasts/sync (docs/podcast-sync-contract.md §6.2).
const (
	// MaxPodcastSyncEdits bounds the register edits of one request; a
	// subscription item is one edit, an episode item one or two.
	MaxPodcastSyncEdits = 500
	// MaxPodcastSyncLimit is the largest page a client may ask for.
	MaxPodcastSyncLimit = 1000
	// maxSyncKeyBytes bounds feed and episode keys.
	maxSyncKeyBytes = 2100
	// maxSyncTitleRunes bounds a subscription's display title.
	maxSyncTitleRunes = 500
	// maxSyncURLs bounds a subscription's urls.
	maxSyncURLs = 10
	// maxObservedMS is the last millisecond of year 9999.
	maxObservedMS = 253402300799999
	// syncClockSkew is how far ahead of the server a clock may be before it
	// is replaced by the receive time (rule 4 of §4): a device with a future
	// clock would otherwise block every later edit.
	syncClockSkew = 5 * time.Minute
)

// ErrInvalidPodcastSync rejects a malformed sync envelope (HTTP 400).
// Invalid single edits are rejected per item in the response instead.
var ErrInvalidPodcastSync = &apiError{msg: "invalid podcast sync request", status: http.StatusBadRequest}

// ErrPodcastEpisodeSyncUnsupported refuses episode edits and scoped
// requests until episode state synchronization exists (task wh3). Nothing
// is acked, so a client keeps those edits pending. The server does not
// advertise the podcast_sync capability before then.
var ErrPodcastEpisodeSyncUnsupported = &apiError{msg: "podcast episode sync is not supported yet", status: http.StatusBadRequest}

// PodcastSyncService synchronizes a user's podcast subscriptions.
type PodcastSyncService interface {
	Sync(ctx context.Context, userID int64, req model.PodcastSyncRequest) (*model.PodcastSyncResponse, error)
}

// PodcastSyncStore is the store the sync service needs.
type PodcastSyncStore interface {
	repository.PodcastSubscriptionRepo
}

type podcastSyncService struct {
	store         PodcastSyncStore
	clock         clock.Clock
	checkInterval int
}

var _ PodcastSyncService = (*podcastSyncService)(nil)

// NewPodcastSyncService creates the sync service. checkInterval is the
// refresh interval, in minutes, of catalog feeds a subscription creates.
func NewPodcastSyncService(store PodcastSyncStore, clk clock.Clock, checkInterval int) *podcastSyncService {
	if checkInterval <= 0 {
		checkInterval = 60
	}
	return &podcastSyncService{store: store, clock: clk, checkInterval: checkInterval}
}

// Sync validates the request, applies the valid subscription edits and
// pulls the user's changes. Every call reads and writes only the caller's
// rows. Item-level problems are reported in Rejected; only a malformed
// envelope fails the whole request.
func (s *podcastSyncService) Sync(ctx context.Context, userID int64, req model.PodcastSyncRequest) (*model.PodcastSyncResponse, error) {
	if err := checkSyncEnvelope(req); err != nil {
		return nil, err
	}
	now := s.clock.Now()
	in := model.PodcastSubscriptionSync{Epoch: req.Epoch, Limit: req.Limit, Now: now, CheckIntervalMinutes: s.checkInterval}
	if req.Since != nil {
		in.Since = *req.Since
	}
	rejected := []model.PodcastSyncRejection{}
	var clamped []string
	for _, e := range req.Subscriptions {
		if reason := subscriptionEditProblem(e); reason != "" {
			rejected = append(rejected, model.PodcastSyncRejection{EditID: e.EditID, Reason: reason})
			continue
		}
		e.Title = strings.TrimSpace(e.Title)
		if e.URLs == nil {
			e.URLs = []string{}
		}
		if limit := now.Add(syncClockSkew).UnixMilli(); e.ObservedMS > limit {
			e.ObservedMS = now.UnixMilli()
			clamped = append(clamped, e.EditID)
		}
		in.Edits = append(in.Edits, e)
	}
	out, err := s.store.SyncPodcastSubscriptions(ctx, userID, in)
	if err != nil {
		return nil, err
	}
	out.Rejected = append(rejected, out.Rejected...)
	out.Clamped = append(out.Clamped, clamped...)
	return out, nil
}

// checkSyncEnvelope rejects requests the server cannot process at all.
func checkSyncEnvelope(req model.PodcastSyncRequest) error {
	if req.Limit < 0 || req.Limit > MaxPodcastSyncLimit {
		return ErrInvalidPodcastSync
	}
	if req.Since != nil && *req.Since < 0 {
		return ErrInvalidPodcastSync
	}
	if len(req.Episodes) > 0 || req.Scope != nil {
		return ErrPodcastEpisodeSyncUnsupported
	}
	if len(req.Subscriptions) > MaxPodcastSyncEdits {
		return ErrInvalidPodcastSync
	}
	return nil
}

// subscriptionEditProblem returns the rejection reason of an edit that can
// never be applied, or "".
func subscriptionEditProblem(e model.PodcastSubscriptionEdit) string {
	switch {
	case !validEditID(e.EditID):
		return model.PodcastRejectInvalidEdit
	case !validFeedKeyShape(e.FeedKey):
		return model.PodcastRejectInvalidKey
	case !validSyncURLs(e.FetchURL, e.URLs):
		return model.PodcastRejectInvalidURL
	case !utf8.ValidString(e.Title) || utf8.RuneCountInString(e.Title) > maxSyncTitleRunes:
		return model.PodcastRejectInvalidEdit
	case e.ObservedMS < 1 || e.ObservedMS > maxObservedMS:
		return model.PodcastRejectInvalidTime
	}
	return ""
}

// validEditID reports whether id is 16-64 bytes of [A-Za-z0-9_-].
func validEditID(id string) bool {
	if len(id) < 16 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// validFeedKeyShape accepts a canonical "feed:" key, i.e. one FeedKey
// returns for the key's own location over http or https, and a
// "legacy-feed:<n>" key, whose existence the store checks.
func validFeedKeyShape(key string) bool {
	if len(key) > maxSyncKeyBytes {
		return false
	}
	if n, ok := strings.CutPrefix(key, "legacy-feed:"); ok {
		return isDecimal(n)
	}
	loc, ok := strings.CutPrefix(key, "feed:")
	if !ok || loc == "" {
		return false
	}
	for _, scheme := range []string{"http://", "https://"} {
		if k, err := podcast.FeedKey(scheme + loc); err == nil && k == key {
			return true
		}
	}
	return false
}

// isDecimal reports whether s is a positive decimal number without
// leading zeros, as the server mints legacy keys.
func isDecimal(s string) bool {
	if s == "" || s[0] == '0' || len(s) > 19 {
		return false
	}
	return strings.Trim(s, "0123456789") == ""
}

// validSyncURLs checks the fetch URL (optional) and at most maxSyncURLs
// urls: each must have a feed key, so it could be fetched and resolved.
func validSyncURLs(fetchURL string, urls []string) bool {
	if len(urls) > maxSyncURLs {
		return false
	}
	if fetchURL != "" {
		urls = append([]string{fetchURL}, urls...)
	}
	for _, u := range urls {
		if _, err := podcast.FeedKey(u); err != nil {
			return false
		}
	}
	return true
}
