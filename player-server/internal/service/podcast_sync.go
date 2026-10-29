package service

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/podcast"
	"github.com/snonux/player/internal/repository"
)

// Limits of POST /api/v1/podcasts/sync (docs/podcast-sync-contract.md §6.2).
const (
	// MaxPodcastSyncEdits bounds the register edits of one request; a
	// subscription item is one edit, an episode item up to three.
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
	// legacyFeedKeyPrefix starts the server-minted key of a catalog feed
	// whose stored URL had no feed key (contract §3.1).
	legacyFeedKeyPrefix = "legacy-feed:"
)

// ErrInvalidPodcastSync rejects a malformed sync envelope (HTTP 400).
// Invalid single edits are rejected per item in the response instead.
var ErrInvalidPodcastSync = &apiError{msg: "invalid podcast sync request", status: http.StatusBadRequest}

// PodcastSyncService synchronizes a user's podcast subscriptions and
// episode state.
type PodcastSyncService interface {
	Sync(ctx context.Context, userID int64, req model.PodcastSyncRequest) (*model.PodcastSyncResponse, error)
}

// PodcastSyncStore is the store the sync service needs.
type PodcastSyncStore interface {
	repository.PodcastSyncRepo
}

type podcastSyncService struct {
	store         PodcastSyncStore
	clock         clock.Clock
	checkInterval int
	mediaRoot     string
}

var _ PodcastSyncService = (*podcastSyncService)(nil)

// syncValidation collects the edits a request may apply, the rejections of
// those it may not, and the edits whose clocks were clamped.
type syncValidation struct {
	now      time.Time
	rejected []model.PodcastSyncRejection
	clamped  []string
}

// NewPodcastSyncService creates the sync service. checkInterval is the
// refresh interval, in minutes, of catalog feeds a subscription creates.
// mediaRoot selects the registered library for the managed podcast set.
func NewPodcastSyncService(store PodcastSyncStore, clk clock.Clock, checkInterval int, mediaRoot string) *podcastSyncService {
	if checkInterval <= 0 {
		checkInterval = 60
	}
	return &podcastSyncService{store: store, clock: clk, checkInterval: checkInterval, mediaRoot: mediaRoot}
}

// Sync validates the request, applies the valid subscription and episode
// edits and pulls the user's changes, or the one episode of a scoped
// request. Every call reads and writes only the caller's rows. Item-level
// problems are reported in Rejected; only a malformed envelope fails the
// whole request.
func (s *podcastSyncService) Sync(ctx context.Context, userID int64, req model.PodcastSyncRequest) (*model.PodcastSyncResponse, error) {
	if err := checkSyncEnvelope(req); err != nil {
		return nil, err
	}
	now := s.clock.Now()
	in := model.PodcastSyncBatch{Epoch: req.Epoch, Limit: req.Limit, Scope: req.Scope, Now: now,
		CheckIntervalMinutes: s.checkInterval, MediaRoot: s.mediaRoot}
	if req.Since != nil {
		in.Since = *req.Since
	}
	v := &syncValidation{now: now, rejected: []model.PodcastSyncRejection{}}
	in.Subscriptions = v.subscriptions(req.Subscriptions)
	in.Episodes = v.episodes(req.Episodes)
	out, err := s.store.SyncPodcast(ctx, userID, in)
	if err != nil {
		return nil, err
	}
	out.Clamped = append(out.Clamped, v.clampedNotRejected(out.Rejected)...)
	out.Rejected = append(v.rejected, out.Rejected...)
	return out, nil
}

// clampedNotRejected returns the clamped edit ids the store did not reject
// (unknown legacy key, quota): a rejected edit was not applied, so no
// clamped clock of it exists to report.
func (v *syncValidation) clampedNotRejected(rejected []model.PodcastSyncRejection) []string {
	gone := make(map[string]bool, len(rejected))
	for _, r := range rejected {
		gone[r.EditID] = true
	}
	kept := []string{}
	for _, id := range v.clamped {
		if !gone[id] {
			kept = append(kept, id)
		}
	}
	return kept
}

// reject records a rejected edit.
func (v *syncValidation) reject(editID, reason string) {
	v.rejected = append(v.rejected, model.PodcastSyncRejection{EditID: editID, Reason: reason})
}

// clamp returns ms, or the server's time when ms is more than
// model.PodcastClockSkew ahead of it (contract §4 rule 4), recording the
// edit as clamped.
func (v *syncValidation) clamp(editID string, ms int64) int64 {
	if ms > v.now.Add(model.PodcastClockSkew).UnixMilli() {
		v.clamped = append(v.clamped, editID)
		return v.now.UnixMilli()
	}
	return ms
}

// subscriptions returns the valid subscription edits, normalized.
func (v *syncValidation) subscriptions(edits []model.PodcastSubscriptionEdit) []model.PodcastSubscriptionEdit {
	var valid []model.PodcastSubscriptionEdit
	for _, e := range edits {
		if reason := subscriptionEditProblem(e); reason != "" {
			v.reject(e.EditID, reason)
			continue
		}
		e.Title = strings.TrimSpace(e.Title)
		if e.URLs == nil {
			e.URLs = []string{}
		}
		e.ObservedMS = v.clamp(e.EditID, e.ObservedMS)
		valid = append(valid, e)
	}
	return valid
}

// checkSyncEnvelope rejects requests the server cannot process at all: bad
// pull parameters, too many edits, an episode item without any edit, or a
// malformed scope.
func checkSyncEnvelope(req model.PodcastSyncRequest) error {
	if req.Limit < 0 || req.Limit > MaxPodcastSyncLimit {
		return ErrInvalidPodcastSync
	}
	if req.Since != nil && *req.Since < 0 {
		return ErrInvalidPodcastSync
	}
	edits := len(req.Subscriptions)
	for _, e := range req.Episodes {
		n := len(episodeItemIDs(e))
		if n == 0 {
			return ErrInvalidPodcastSync
		}
		edits += n
	}
	if edits > MaxPodcastSyncEdits {
		return ErrInvalidPodcastSync
	}
	return checkSyncScope(req)
}

// checkSyncScope accepts a scope only instead of a cursor and only with
// keys of a valid shape.
func checkSyncScope(req model.PodcastSyncRequest) error {
	if req.Scope == nil {
		return nil
	}
	if req.Since != nil || !validFeedKeyShape(req.Scope.FeedKey) || !validEpisodeKeyShape(req.Scope.EpisodeKey) {
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
	if n, ok := strings.CutPrefix(key, legacyFeedKeyPrefix); ok {
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
