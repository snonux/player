package model

import "time"

// MusicLibrary is an independent filesystem root that owns sets.
type MusicLibrary struct {
	ID        int64              `json:"id"`
	Name      string             `json:"name"`
	RootPath  string             `json:"root_path"`
	CreatedAt time.Time          `json:"created_at"`
	Stats     *MusicLibraryStats `json:"stats,omitempty"`
}

// MusicLibraryStats aggregates media under one library for admin/browse.
type MusicLibraryStats struct {
	SetCount      int     `json:"set_count"`
	SongCount     int     `json:"song_count"`
	TotalBytes    int64   `json:"total_bytes"`
	TotalDuration float64 `json:"total_duration"`
}
