package model

import "time"

// ListeningStats describes qualified music plays by the signed-in account.
// The existing media play_count remains a separate global counter.
type ListeningStats struct {
	PersonalPlayCount int        `json:"personal_play_count"`
	LastPlayedAt      *time.Time `json:"last_played_at,omitempty"`
}

// Add combines caller-visible songs, taking the latest qualified play date.
func (s *ListeningStats) Add(other ListeningStats) {
	s.PersonalPlayCount += other.PersonalPlayCount
	if other.LastPlayedAt != nil && (s.LastPlayedAt == nil || other.LastPlayedAt.After(*s.LastPlayedAt)) {
		date := *other.LastPlayedAt
		s.LastPlayedAt = &date
	}
}
