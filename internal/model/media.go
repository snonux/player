// Package model defines the core domain entities.
package model

import "time"

// MediaType distinguishes between video and audio files.
type MediaType string

const (
	// MediaTypeVideo identifies video files.
	MediaTypeVideo MediaType = "video"
	// MediaTypeAudio identifies audio files.
	MediaTypeAudio MediaType = "audio"
	// MediaTypeImage identifies image files.
	MediaTypeImage MediaType = "image"
)

// Role defines the level of access a user has to a set.
type Role string

const (
	// RoleOwner allows browsing, uploading, deleting and thumbnail updates.
	RoleOwner Role = "owner"
	// RoleViewer allows browsing and playback.
	RoleViewer Role = "viewer"
)

// User represents an application account.
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	IsAdmin      bool      `json:"is_admin"`
	CreatedAt    time.Time `json:"created_at"`
}

// Set represents a top-level media collection (a directory under MEDIA_ROOT).
type Set struct {
	ID                 int64           `json:"id"`
	Name               string          `json:"name"`
	RootPath           string          `json:"root_path"`
	CoverThumbnailPath string          `json:"cover_thumbnail_path"`
	IsPodcast          bool            `json:"is_podcast"`
	Permissions        []SetPermission `json:"permissions"`
	CreatedAt          time.Time       `json:"created_at"`
}

// SetPermission grants a user access to a set.
type SetPermission struct {
	SetID     int64     `json:"set_id"`
	UserID    int64     `json:"user_id"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// Media represents a single media file within a set.
type Media struct {
	ID              int64      `json:"id"`
	SetID           int64      `json:"set_id"`
	RelPath         string     `json:"rel_path"`
	FileName        string     `json:"file_name"`
	AbsPath         string     `json:"abs_path"`
	Type            MediaType  `json:"type"`
	Duration        float64    `json:"duration"`
	Codec           string     `json:"codec"`
	Resolution      string     `json:"resolution"`
	Bitrate         int        `json:"bitrate"`
	FileSizeBytes   int64      `json:"file_size_bytes"`
	Width           int        `json:"width"`
	Height          int        `json:"height"`
	EXIFCamera      string     `json:"exif_camera"`
	EXIFLens        string     `json:"exif_lens"`
	EXIFDate        string     `json:"exif_date"`
	EXIFISO         string     `json:"exif_iso"`
	EXIFFNumber     string     `json:"exif_f_number"`
	EXIFExposure    string     `json:"exif_exposure"`
	EXIFFocalLength string     `json:"exif_focal_length"`
	ThumbnailPath   string     `json:"thumbnail_path"`
	PlayCount       int        `json:"play_count"`
	DeletedAt       *time.Time `json:"deleted_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

// Tag is a label that can be attached to media items.
type Tag struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Session is an authenticated browser session.
type Session struct {
	ID        string    `json:"id"`
	UserID    int64     `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// Share is a time-bounded public link to a media item.
type Share struct {
	Token     string    `json:"token"`
	MediaID   int64     `json:"media_id"`
	CreatedBy int64     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	MaxUses   *int      `json:"max_uses"`
	UsedCount int       `json:"used_count"`
}

// Note is a per-user, per-media text note.
type Note struct {
	ID        int64     `json:"id"`
	MediaID   int64     `json:"media_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PlaybackProgress stores the last known playback position.
type PlaybackProgress struct {
	UserID          int64     `json:"user_id"`
	MediaID         int64     `json:"media_id"`
	PositionSeconds float64   `json:"position_seconds"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PlaybackAccumulator tracks deltas for the 60-second playback counter rule.
type PlaybackAccumulator struct {
	SessionID          string    `json:"session_id"`
	MediaID            int64     `json:"media_id"`
	LastPosition       float64   `json:"last_position"`
	AccumulatedSeconds float64   `json:"accumulated_seconds"`
	Counted            bool      `json:"counted"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Favorite records that a user has favorited a media item.
type Favorite struct {
	UserID    int64     `json:"user_id"`
	MediaID   int64     `json:"media_id"`
	CreatedAt time.Time `json:"created_at"`
}

// MediaTag is the join table between media and tags.
type MediaTag struct {
	MediaID int64 `json:"media_id"`
	TagID   int64 `json:"tag_id"`
}

// Metadata holds extracted file properties from ffprobe and os.Stat.
type Metadata struct {
	Duration        float64 `json:"duration"`
	Codec           string  `json:"codec"`
	Resolution      string  `json:"resolution"`
	Bitrate         int     `json:"bitrate"`
	FileSizeBytes   int64   `json:"file_size_bytes"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	EXIFCamera      string  `json:"exif_camera"`
	EXIFLens        string  `json:"exif_lens"`
	EXIFDate        string  `json:"exif_date"`
	EXIFISO         string  `json:"exif_iso"`
	EXIFFNumber     string  `json:"exif_f_number"`
	EXIFExposure    string  `json:"exif_exposure"`
	EXIFFocalLength string  `json:"exif_focal_length"`
}
