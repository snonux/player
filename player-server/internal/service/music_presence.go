package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
)

const presenceTTL = 45 * time.Second

// PresenceStore supplies current identity and credential validity.
type PresenceStore interface {
	GetUserByID(context.Context, int64) (*model.User, error)
	GetSessionByID(context.Context, string) (*model.Session, error)
	ListByUser(context.Context, int64) ([]model.APIToken, error)
}

// PresenceUpdate is an explicit opt-in heartbeat from one client.
type PresenceUpdate struct {
	ClientID string  `json:"client_id"`
	Client   string  `json:"client"`
	MediaID  int64   `json:"media_id"`
	State    string  `json:"state"`
	Position float64 `json:"position_seconds"`
	Sequence int64   `json:"sequence"`
}

// MusicPresence contains only caller-visible, freshly authorized metadata.
type MusicPresence struct {
	Username  string    `json:"username"`
	Client    string    `json:"client"`
	MediaID   int64     `json:"media_id"`
	Title     string    `json:"title"`
	Artist    string    `json:"artist"`
	State     string    `json:"state"`
	Position  float64   `json:"position_seconds"`
	Duration  float64   `json:"duration"`
	UpdatedAt time.Time `json:"updated_at"`
}

type presencePublisher struct {
	user  *model.User
	songs map[int64]model.Media
}

type presenceKey struct {
	user   int64
	client string
}
type presenceEntry struct {
	update  PresenceUpdate
	session model.Session
	updated time.Time
	active  bool
}

// MusicPresenceService owns bounded ephemeral client presence, separate from listening history.
type MusicPresenceService struct {
	mu      sync.Mutex
	entries map[presenceKey]presenceEntry
	store   PresenceStore
	source  CatalogSource
	clock   clock.Clock
}

// NewMusicPresenceService constructs process-local listener presence.
func NewMusicPresenceService(store PresenceStore, source CatalogSource, clk clock.Clock) *MusicPresenceService {
	return &MusicPresenceService{entries: make(map[presenceKey]presenceEntry), store: store, source: source, clock: clk}
}

// Update publishes current music only after checking the publisher's present ACL.
func (s *MusicPresenceService) Update(ctx context.Context, session model.Session, update PresenceUpdate) error {
	if !validPresenceClient(update.ClientID) || update.Sequence <= 0 || (update.Client != "web" && update.Client != "android") || (update.State != "playing" && update.State != "paused") || update.MediaID <= 0 || math.IsNaN(update.Position) || math.IsInf(update.Position, 0) || update.Position < 0 {
		return &apiError{msg: "Invalid listener heartbeat.", status: 400}
	}
	songs, err := s.songs(ctx, session.UserID)
	if err != nil {
		return err
	}
	song, ok := songs[update.MediaID]
	if !ok {
		return ErrNotFound
	}
	update.Position = clampPresencePosition(update.Position, song.Duration)
	return s.save(session, update, true)
}

// Remove records a sequence tombstone so delayed requests cannot restore sharing.
func (s *MusicPresenceService) Remove(session model.Session, client string, sequence int64) error {
	if !validPresenceClient(client) || sequence <= 0 {
		return &apiError{msg: "Invalid listener client.", status: 400}
	}
	return s.save(session, PresenceUpdate{ClientID: client, Sequence: sequence}, false)
}

// List revalidates both accounts, their music permissions, and the originating credential.
func (s *MusicPresenceService) List(ctx context.Context, user int64) ([]MusicPresence, error) {
	result := []MusicPresence{}
	viewer, err := s.songs(ctx, user)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.prune()
	entries := make([]presenceEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		if entry.active {
			entries = append(entries, entry)
		}
	}
	s.mu.Unlock()
	publishers := make(map[int64]presencePublisher)
	for _, entry := range entries {
		song, ok := viewer[entry.update.MediaID]
		if !ok {
			continue
		}
		valid, err := s.validSession(ctx, entry.session)
		if err != nil {
			return nil, err
		}
		if !valid {
			s.discard(entry)
			continue
		}
		publisher, loaded := publishers[entry.session.UserID]
		if !loaded {
			publisher.user, err = s.store.GetUserByID(ctx, entry.session.UserID)
			if err != nil {
				return nil, fmt.Errorf("listener user: %w", err)
			}
			if publisher.user != nil {
				if entry.session.UserID == user {
					publisher.songs = viewer
				} else {
					publisher.songs, err = s.songs(ctx, entry.session.UserID)
					if err != nil {
						return nil, err
					}
				}
			}
			publishers[entry.session.UserID] = publisher
		}
		if publisher.user == nil {
			s.discard(entry)
			continue
		}
		if _, ok := publisher.songs[song.ID]; !ok {
			s.discard(entry)
			continue
		}
		// Opt-out/account changes during I/O must invalidate the captured entry.
		s.mu.Lock()
		current, exists := s.entries[presenceKey{entry.session.UserID, entry.update.ClientID}]
		currentEntry := exists && current.active && current.update.Sequence == entry.update.Sequence && current.session.ID == entry.session.ID && s.clock.Now().Sub(current.updated) < presenceTTL
		s.mu.Unlock()
		if !currentEntry {
			continue
		}
		title, artist := song.FileName, ""
		if song.Music != nil {
			if song.Music.Title != "" {
				title = song.Music.Title
			}
			artist = song.Music.Artist
		}
		client := "Web"
		if entry.update.Client == "android" {
			client = "Android"
		}
		result = append(result, MusicPresence{Username: publisher.user.Username, Client: client, MediaID: song.ID, Title: title, Artist: artist, State: entry.update.State, Position: clampPresencePosition(entry.update.Position, song.Duration), Duration: song.Duration, UpdatedAt: entry.updated})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Username != result[j].Username {
			return result[i].Username < result[j].Username
		}
		return result[i].UpdatedAt.Before(result[j].UpdatedAt)
	})
	return result, nil
}

func (s *MusicPresenceService) save(session model.Session, update PresenceUpdate, active bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	key := presenceKey{session.UserID, update.ClientID}
	old, exists := s.entries[key]
	if exists && update.Sequence <= old.update.Sequence {
		return nil
	}
	if !exists {
		count := 0
		for key := range s.entries {
			if key.user == session.UserID {
				count++
			}
		}
		if len(s.entries) >= 256 || count >= 8 {
			return &apiError{msg: "Too many listener clients. Wait a moment and retry.", status: 429}
		}
	}
	s.entries[key] = presenceEntry{update: update, session: session, updated: s.clock.Now(), active: active}
	return nil
}

func (s *MusicPresenceService) prune() {
	now := s.clock.Now()
	for key, entry := range s.entries {
		if now.Sub(entry.updated) >= presenceTTL {
			delete(s.entries, key)
		}
	}
}

func (s *MusicPresenceService) songs(ctx context.Context, user int64) (map[int64]model.Media, error) {
	albums, err := MusicAlbums(ctx, s.source, user)
	if err != nil {
		return nil, err
	}
	result := make(map[int64]model.Media)
	for _, album := range albums {
		for _, song := range album.Songs {
			result[song.ID] = song
		}
	}
	return result, nil
}

func (s *MusicPresenceService) validSession(ctx context.Context, session model.Session) (bool, error) {
	now := s.clock.Now()
	if !session.ExpiresAt.IsZero() && !session.ExpiresAt.After(now) {
		return false, nil
	}
	if strings.HasPrefix(session.ID, "api-token:") {
		id, err := strconv.ParseInt(strings.TrimPrefix(session.ID, "api-token:"), 10, 64)
		if err != nil {
			return false, nil
		}
		tokens, err := s.store.ListByUser(ctx, session.UserID)
		if err != nil {
			return false, fmt.Errorf("listener tokens: %w", err)
		}
		for _, token := range tokens {
			if token.ID == id && token.UserID == session.UserID && (token.ExpiresAt == nil || token.ExpiresAt.After(now)) {
				return true, nil
			}
		}
		return false, nil
	}
	current, err := s.store.GetSessionByID(ctx, session.ID)
	if err != nil {
		return false, fmt.Errorf("listener session: %w", err)
	}
	return current != nil && current.UserID == session.UserID && current.ExpiresAt.After(now), nil
}

func validPresenceClient(value string) bool {
	if len(value) < 16 || len(value) > 80 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-') {
			return false
		}
	}
	return true
}

func clampPresencePosition(position, duration float64) float64 {
	if duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return 0
	}
	return math.Min(position, duration)
}

func (s *MusicPresenceService) discard(entry presenceEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := presenceKey{entry.session.UserID, entry.update.ClientID}
	current, ok := s.entries[key]
	if ok && current.update.Sequence == entry.update.Sequence && current.session.ID == entry.session.ID {
		current.active = false
		s.entries[key] = current
	}
}
