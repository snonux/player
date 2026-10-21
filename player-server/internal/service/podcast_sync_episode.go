package service

import (
	"math"
	"strings"
	"unicode/utf8"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/podcast"
)

// Validation of the episode items of a sync request
// (docs/podcast-sync-contract.md §6.2). Each item carries up to two edits,
// one per register; they are rejected independently, except that a
// problem with the item's keys or enclosure URL rejects both.

// maxSyncPositionSeconds bounds a position, as SyncPlayback does.
const maxSyncPositionSeconds = 9e12

// asciiSpace is the whitespace podcast identities trim (contract §3.1).
const asciiSpace = " \t\r\n\f\v"

// episodes returns the episode items with every invalid register removed
// and the valid clocks clamped; an item left without registers is dropped.
func (v *syncValidation) episodes(items []model.PodcastEpisodeEdit) []model.PodcastEpisodeEdit {
	var valid []model.PodcastEpisodeEdit
	for _, item := range items {
		itemProblem := episodeItemProblem(item)
		item.Position = v.position(item.Position, itemProblem)
		item.Completion = v.completion(item.Completion, itemProblem)
		if item.Position != nil || item.Completion != nil {
			valid = append(valid, item)
		}
	}
	return valid
}

// position returns a valid position edit with its clock clamped, or nil
// after rejecting an invalid one.
func (v *syncValidation) position(p *model.PodcastPositionEdit, itemProblem string) *model.PodcastPositionEdit {
	if p == nil {
		return nil
	}
	if reason := firstProblem(editIDProblem(p.EditID), itemProblem, positionProblem(*p)); reason != "" {
		v.reject(p.EditID, reason)
		return nil
	}
	edit := *p
	edit.ObservedMS = v.clamp(p.EditID, p.ObservedMS)
	return &edit
}

// completion returns a valid completion edit with its clock clamped, or
// nil after rejecting an invalid one.
func (v *syncValidation) completion(c *model.PodcastCompletionEdit, itemProblem string) *model.PodcastCompletionEdit {
	if c == nil {
		return nil
	}
	if reason := firstProblem(editIDProblem(c.EditID), itemProblem, completionProblem(*c)); reason != "" {
		v.reject(c.EditID, reason)
		return nil
	}
	edit := *c
	edit.ObservedMS = v.clamp(c.EditID, c.ObservedMS)
	return &edit
}

// firstProblem returns the first non-empty rejection reason.
func firstProblem(reasons ...string) string {
	for _, r := range reasons {
		if r != "" {
			return r
		}
	}
	return ""
}

// editIDProblem rejects an edit id of the wrong shape.
func editIDProblem(id string) string {
	if !validEditID(id) {
		return model.PodcastRejectInvalidEdit
	}
	return ""
}

// episodeItemProblem checks what both edits of an item share: the keys
// and the optional enclosure URL, which must canonicalize like any
// enclosure (contract §3.2) to count as evidence.
func episodeItemProblem(item model.PodcastEpisodeEdit) string {
	if !validFeedKeyShape(item.FeedKey) || !validEpisodeKeyShape(item.EpisodeKey) {
		return model.PodcastRejectInvalidKey
	}
	if item.EnclosureURL != "" {
		if _, err := podcast.EnclosureKey(item.EnclosureURL); err != nil {
			return model.PodcastRejectInvalidURL
		}
	}
	return ""
}

// positionProblem checks a position edit's value and clock.
func positionProblem(p model.PodcastPositionEdit) string {
	if math.IsNaN(p.Seconds) || math.IsInf(p.Seconds, 0) || p.Seconds < 0 || p.Seconds > maxSyncPositionSeconds {
		return model.PodcastRejectInvalidPosition
	}
	return clockProblem(p.ObservedMS)
}

// completionProblem checks a completion edit's source and clock.
func completionProblem(c model.PodcastCompletionEdit) string {
	if c.Source != model.PodcastCompletionManual && c.Source != model.PodcastCompletionAuto {
		return model.PodcastRejectInvalidEdit
	}
	return clockProblem(c.ObservedMS)
}

// clockProblem rejects a clock outside 1 … the last millisecond of 9999.
func clockProblem(ms int64) string {
	if ms < 1 || ms > maxObservedMS {
		return model.PodcastRejectInvalidTime
	}
	return ""
}

// episodeItemIDs returns the edit ids of an item's registers.
func episodeItemIDs(item model.PodcastEpisodeEdit) []string {
	var ids []string
	if item.Position != nil {
		ids = append(ids, item.Position.EditID)
	}
	if item.Completion != nil {
		ids = append(ids, item.Completion.EditID)
	}
	return ids
}

// validEpisodeKeyShape accepts the episode key shapes of contract §3.2:
// "guid:" with a trimmed, non-empty, valid UTF-8 GUID of at most 1024
// bytes (or its "guid:sha256:" hash form), a canonical "enc:" key, and a
// server-minted "legacy-episode:<n>" key, whose existence the store checks.
func validEpisodeKeyShape(key string) bool {
	if len(key) > maxSyncKeyBytes {
		return false
	}
	if n, ok := strings.CutPrefix(key, "legacy-episode:"); ok {
		return isDecimal(n)
	}
	if guid, ok := strings.CutPrefix(key, "guid:"); ok {
		return guid != "" && utf8.ValidString(guid) && strings.Trim(guid, asciiSpace) == guid &&
			podcast.GUIDKey(guid) == key
	}
	loc, ok := strings.CutPrefix(key, "enc:")
	if !ok || loc == "" {
		return false
	}
	for _, scheme := range []string{"http://", "https://"} {
		if k, err := podcast.EnclosureKey(scheme + loc); err == nil && k == key {
			return true
		}
	}
	return false
}
