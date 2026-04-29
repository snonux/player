// Package model defines the core domain entities.
package model

import "time"

// MediaType distinguishes between video and audio files.
type MediaType string

const (
	MediaTypeVideo MediaType = "video"
	MediaTypeAudio MediaType = "audio"
)

// Role defines the level of access a user has to a set.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleViewer Role = "viewer"
)

// User represents an application account.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    time.Time
}

// Set represents a top-level media collection (a directory under MEDIA_ROOT).
type Set struct {
	ID                 int64
	Name               string
	RootPath           string
	CoverThumbnailPath string
	Permissions        []SetPermission
	CreatedAt          time.Time
}

// SetPermission grants a user access to a set.
type SetPermission struct {
	SetID     int64
	UserID    int64
	Role      Role
	CreatedAt time.Time
}

// Media represents a single audio or video file within a set.
type Media struct {
	ID            int64
	SetID         int64
	RelPath       string
	FileName      string
	AbsPath       string
	Type          MediaType
	Duration      float64
	Codec         string
	Resolution    string
	Bitrate       int
	FileSizeBytes int64
	ThumbnailPath string
	PlayCount     int
	DeletedAt     *time.Time
	CreatedAt     time.Time
}

// Tag is a label that can be attached to media items.
type Tag struct {
	ID   int64
	Name string
}

// Session is an authenticated browser session.
type Session struct {
	ID        string
	UserID    int64
	ExpiresAt time.Time
	CreatedAt time.Time
}

// Share is a time-bounded public link to a media item.
type Share struct {
	Token     string
	MediaID   int64
	CreatedBy int64
	CreatedAt time.Time
	ExpiresAt time.Time
	MaxUses   *int
	UsedCount int
}

// Note is a per-user, per-media text note.
type Note struct {
	ID        int64
	MediaID   int64
	UserID    int64
	Content   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PlaybackProgress stores the last known playback position.
type PlaybackProgress struct {
	UserID          int64
	MediaID         int64
	PositionSeconds float64
	UpdatedAt       time.Time
}

// PlaybackAccumulator tracks deltas for the 60-second playback counter rule.
type PlaybackAccumulator struct {
	SessionID          string
	MediaID            int64
	LastPosition       float64
	AccumulatedSeconds float64
	Counted            bool
	UpdatedAt          time.Time
}

// Favorite records that a user has favorited a media item.
type Favorite struct {
	UserID    int64
	MediaID   int64
	CreatedAt time.Time
}

// MediaTag is the join table between media and tags.
type MediaTag struct {
	MediaID int64
	TagID   int64
}

// Metadata holds extracted file properties from ffprobe and os.Stat.
type Metadata struct {
	Duration      float64
	Codec         string
	Resolution    string
	Bitrate       int
	FileSizeBytes int64
}
