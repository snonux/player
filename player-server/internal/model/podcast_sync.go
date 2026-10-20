package model

import "time"

// Podcast synchronization wire types (docs/podcast-sync-contract.md, §6.2).
// All clocks are UTC Unix milliseconds captured when the user made the edit;
// they are never stamped with send, receive or migration time.

// Rejection reasons of the podcast sync API. A rejected edit can never
// succeed, so clients drop it from their queue.
const (
	PodcastRejectInvalidKey      = "invalid_key"
	PodcastRejectInvalidURL      = "invalid_url"
	PodcastRejectInvalidPosition = "invalid_position"
	PodcastRejectInvalidTime     = "invalid_time"
	PodcastRejectInvalidEdit     = "invalid_edit"
	PodcastRejectQuota           = "quota"
)

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
// registers. Applying them is not implemented yet; the type documents the
// wire shape so a request carrying episodes can be refused explicitly.
type PodcastEpisodeEdit struct {
	FeedKey      string                 `json:"feed_key"`
	EpisodeKey   string                 `json:"episode_key"`
	EnclosureURL string                 `json:"enclosure_url"`
	Position     *PodcastPositionEdit   `json:"position"`
	Completion   *PodcastCompletionEdit `json:"completion"`
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
	NextSince     int64                  `json:"next_since"`
	HasMore       bool                   `json:"has_more"`
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

// PodcastSubscriptionSync is what the service hands the store after
// validation: edits that passed the shape checks (clocks already clamped)
// and the pull parameters.
type PodcastSubscriptionSync struct {
	Epoch string
	Since int64
	Limit int
	Edits []PodcastSubscriptionEdit
	// Now is the server time, used for catalog rows a subscription creates
	// and for their per-user hourly limit.
	Now time.Time
	// CheckIntervalMinutes is the refresh interval of new catalog rows.
	CheckIntervalMinutes int
}
