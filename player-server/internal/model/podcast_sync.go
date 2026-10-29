package model

import "time"

// Podcast synchronization wire types (docs/podcast-sync-contract.md, §6.2).
// All clocks are UTC Unix milliseconds captured when the user made the edit;
// they are never stamped with send, receive or migration time.

// Rejection reasons of the podcast sync API. Clients record rejections for
// diagnosis. Episode deletion edits remain pending for a later bounded retry
// until acknowledged, including when server quota temporarily prevents them.
const (
	PodcastRejectInvalidKey      = "invalid_key"
	PodcastRejectInvalidURL      = "invalid_url"
	PodcastRejectInvalidPosition = "invalid_position"
	PodcastRejectInvalidTime     = "invalid_time"
	PodcastRejectInvalidEdit     = "invalid_edit"
	PodcastRejectQuota           = "quota"
)

// Completion sources of the completion register: a manual action of the
// user, or the player's natural end of the media (contract §4.2).
const (
	PodcastCompletionManual = "manual"
	PodcastCompletionAuto   = "auto"
)

// PodcastClockSkew is how far ahead of the server's own time an edit clock
// may be before the server replaces it with its receive time (contract §4
// rule 4): a device with a future clock would otherwise block every later
// edit of that register.
const PodcastClockSkew = 5 * time.Minute

// Kinds of a PodcastKeyMapping.
const (
	PodcastKeyKindFeed    = "feed"
	PodcastKeyKindEpisode = "episode"
)

// PodcastSyncRequest is the body of POST /api/v1/podcasts/sync.
type PodcastSyncRequest struct {
	Epoch string `json:"epoch"`
	// Since is the change cursor of the last applied page; nil means 0.
	Since *int64 `json:"since"`
	// Limit caps the pulled rows; 0 means the default.
	Limit         int                       `json:"limit"`
	Scope         *PodcastSyncScope         `json:"scope"`
	Subscriptions []PodcastSubscriptionEdit `json:"subscriptions"`
	Episodes      []PodcastEpisodeEdit      `json:"episodes"`
}

// PodcastSyncScope asks for the current registers of one episode only.
type PodcastSyncScope struct {
	FeedKey    string `json:"feed_key"`
	EpisodeKey string `json:"episode_key"`
}

// PodcastSubscriptionEdit is one edit of a user's subscription register.
type PodcastSubscriptionEdit struct {
	EditID     string   `json:"edit_id"`
	FeedKey    string   `json:"feed_key"`
	URLs       []string `json:"urls"`
	FetchURL   string   `json:"fetch_url"`
	Title      string   `json:"title"`
	Subscribed bool     `json:"subscribed"`
	ObservedMS int64    `json:"observed_ms"`
}

// PodcastEpisodeEdit carries edits of an episode's position and completion
// registers; at least one of them is set. EnclosureURL is evidence for
// resolving an enc: or unknown key, never an identity of its own.
type PodcastEpisodeEdit struct {
	FeedKey      string                 `json:"feed_key"`
	EpisodeKey   string                 `json:"episode_key"`
	EnclosureURL string                 `json:"enclosure_url"`
	Position     *PodcastPositionEdit   `json:"position"`
	Completion   *PodcastCompletionEdit `json:"completion"`
	Deletion     *PodcastDeletionEdit   `json:"deletion"`
}

// PodcastPositionEdit is one edit of the position register.
type PodcastPositionEdit struct {
	EditID     string  `json:"edit_id"`
	Seconds    float64 `json:"seconds"`
	ObservedMS int64   `json:"observed_ms"`
}

// PodcastCompletionEdit is one edit of the completion register.
type PodcastCompletionEdit struct {
	EditID     string `json:"edit_id"`
	Completed  bool   `json:"completed"`
	Source     string `json:"source"`
	ObservedMS int64  `json:"observed_ms"`
}

type PodcastDeletionEdit struct {
	EditID     string `json:"edit_id"`
	Deleted    bool   `json:"deleted"`
	ObservedMS int64  `json:"observed_ms"`
}

type PodcastDeletionRegister struct {
	Deleted    bool  `json:"deleted"`
	ObservedMS int64 `json:"observed_ms"`
}

// PodcastSyncResponse is the answer of POST /api/v1/podcasts/sync.
type PodcastSyncResponse struct {
	Epoch         string                 `json:"epoch"`
	Acked         []string               `json:"acked"`
	Rejected      []PodcastSyncRejection `json:"rejected"`
	Clamped       []string               `json:"clamped"`
	Resolved      []PodcastKeyMapping    `json:"resolved"`
	Subscriptions []PodcastSubscription  `json:"subscriptions"`
	Episodes      []PodcastEpisodeState  `json:"episodes"`
	Aliases       []PodcastKeyMapping    `json:"aliases"`
	// NextSince and HasMore are the cursor fields of a pull. A scoped
	// request (one episode, no cursor) leaves both nil, so they are omitted.
	NextSince *int64 `json:"next_since,omitempty"`
	HasMore   *bool  `json:"has_more,omitempty"`
}

// PodcastSyncRejection names a rejected edit and why.
type PodcastSyncRejection struct {
	EditID string `json:"edit_id"`
	Reason string `json:"reason"`
}

// PodcastKeyMapping maps a key to its canonical key: a resolution the server
// applied to an incoming key, or a pulled alias. FeedKey is set for episode
// mappings only.
type PodcastKeyMapping struct {
	Kind    string `json:"kind"`
	FeedKey string `json:"feed_key,omitempty"`
	From    string `json:"from"`
	To      string `json:"to"`
}

// PodcastSubscription is a user's subscription register for one feed.
// Subscribed false is a tombstone, kept forever.
type PodcastSubscription struct {
	FeedKey        string   `json:"feed_key"`
	Subscribed     bool     `json:"subscribed"`
	SubscriptionMS int64    `json:"subscription_ms"`
	Title          string   `json:"title"`
	FetchURL       string   `json:"fetch_url"`
	URLs           []string `json:"urls"`
}

// PodcastEpisodeState holds a user's registers of one episode. A register
// that was never set is nil.
type PodcastEpisodeState struct {
	FeedKey    string                     `json:"feed_key"`
	EpisodeKey string                     `json:"episode_key"`
	Position   *PodcastPositionRegister   `json:"position,omitempty"`
	Completion *PodcastCompletionRegister `json:"completion,omitempty"`
	Deletion   *PodcastDeletionRegister   `json:"deletion,omitempty"`
}

// PodcastPositionRegister is the stored position register.
type PodcastPositionRegister struct {
	Seconds    float64 `json:"seconds"`
	ObservedMS int64   `json:"observed_ms"`
}

// PodcastCompletionRegister is the stored completion register.
type PodcastCompletionRegister struct {
	Completed  bool   `json:"completed"`
	Source     string `json:"source"`
	ObservedMS int64  `json:"observed_ms"`
}

// PodcastSyncBatch is what the sync service hands the store after
// validation: edits that passed the shape checks (clocks already clamped)
// and either the pull parameters or a scope.
type PodcastSyncBatch struct {
	// MediaRoot identifies the registered default library for managed podcasts.
	MediaRoot string
	Epoch     string
	Since     int64
	Limit     int
	// Scope, when set, replaces the cursor pull by the current registers
	// of that one episode (the pre-playback request).
	Scope         *PodcastSyncScope
	Subscriptions []PodcastSubscriptionEdit
	// Episodes holds only the registers that passed validation; a
	// rejected register is nil.
	Episodes []PodcastEpisodeEdit
	// Now is the server time, used for catalog rows a subscription creates
	// and for their per-user hourly limit.
	Now time.Time
	// CheckIntervalMinutes is the refresh interval of new catalog rows.
	CheckIntervalMinutes int
}
