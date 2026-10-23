package model

import "time"

// MusicIdentityConflict is an ambiguous move/retag match left for admin recovery.
type MusicIdentityConflict struct {
	ID                int64      `json:"id"`
	LibraryID         int64      `json:"library_id"`
	MissingMediaID    int64      `json:"missing_media_id"`
	CandidateMediaIDs string     `json:"candidate_media_ids"` // JSON array of int64
	MissingPath       string     `json:"missing_path"`
	CandidatePaths    []string   `json:"candidate_paths,omitempty"`
	Reason            string     `json:"reason"`
	CreatedAt         time.Time  `json:"created_at"`
	ResolvedAt        *time.Time `json:"resolved_at,omitempty"`
	Resolution        string     `json:"resolution,omitempty"`
}
