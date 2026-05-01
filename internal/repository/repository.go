// Package repository provides data access abstractions.
package repository

import (
	"context"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// Store is the composite interface for all repositories.
type Store interface {
	UserRepo
	SetRepo
	SetPermissionRepo
	MediaRepo
	TagRepo
	FavoriteRepo
	PlaybackProgressRepo
	PlaybackAccumulatorRepo
	SessionRepo
	ShareRepo
	NoteRepo
}

// MediaServiceStore is the subset of Store required by service.MediaService.
type MediaServiceStore interface {
	UserRepo
	SetRepo
	SetPermissionRepo
	MediaRepo
	TagRepo
	FavoriteRepo
	PlaybackProgressRepo
	ShareRepo
	NoteRepo
}

// AdminServiceStore is the subset of Store required by service.AdminService.
type AdminServiceStore interface {
	UserRepo
	SetRepo
	SetPermissionRepo
	MediaRepo
}

// ProgressServiceStore is the subset of Store required by service.ProgressService.
type ProgressServiceStore interface {
	PlaybackProgressRepo
	PlaybackAccumulatorRepo
	MediaRepo
}

// GCStore is the subset of Store required by service.GCWorker.
type GCStore interface {
	MediaRepo
}

// AuthServiceStore is the subset of Store required by service.AuthService.
type AuthServiceStore interface {
	UserRepo
}

// ScannerStore is the subset of Store required by scanner.FSScanner.
type ScannerStore interface {
	SetRepo
	MediaRepo
}

// UserRepo manages application users.
type UserRepo interface {
	CreateUser(ctx context.Context, user *model.User) (int64, error)
	GetUserByID(ctx context.Context, id int64) (*model.User, error)
	GetUserByUsername(ctx context.Context, username string) (*model.User, error)
	ListUsers(ctx context.Context) ([]model.User, error)
	DeleteUser(ctx context.Context, id int64) error
	CountUsers(ctx context.Context) (int, error)
}

// SetRepo manages media sets.
type SetRepo interface {
	CreateSet(ctx context.Context, set *model.Set) (int64, error)
	GetSetByID(ctx context.Context, id int64) (*model.Set, error)
	ListSets(ctx context.Context) ([]model.Set, error)
	UpdateSet(ctx context.Context, set *model.Set) error
	DeleteSet(ctx context.Context, id int64) error
}

// SetPermissionRepo manages set access grants.
type SetPermissionRepo interface {
	GrantPermission(ctx context.Context, perm *model.SetPermission) error
	RevokePermission(ctx context.Context, setID, userID int64) error
	GetPermission(ctx context.Context, setID, userID int64) (*model.SetPermission, error)
	ListPermissionsBySet(ctx context.Context, setID int64) ([]model.SetPermission, error)
	ListPermissionsByUser(ctx context.Context, userID int64) ([]model.SetPermission, error)
}

// MediaFilter defines query parameters for listing media.
type MediaFilter struct {
	SetID         *int64
	SetIDs        []int64 // multi-set selection
	AllowedSetIDs []int64
	Type          *model.MediaType
	Search        string
	Tags          []string
	Favorites     bool // restrict to current user's favorites
	UserID        int64
	MinDuration   *float64
	MaxDuration   *float64
	MinFileSize   *int64
	MaxFileSize   *int64
	Sort          string // name, date, duration, play_count, random
	Limit         int
	Offset        int
}

// MediaRepo manages media items.
type MediaRepo interface {
	CreateMedia(ctx context.Context, media *model.Media) (int64, error)
	UpdateMedia(ctx context.Context, media *model.Media) error
	UpdateMediaThumbnail(ctx context.Context, id int64, thumbnailPath string) error
	GetMediaByID(ctx context.Context, id int64) (*model.Media, error)
	ListMedia(ctx context.Context, filter MediaFilter) ([]model.Media, error)
	SoftDeleteMedia(ctx context.Context, id int64) error
	RestoreMedia(ctx context.Context, id int64) error
	HardDeleteMedia(ctx context.Context, id int64) error
	IncrementPlayCount(ctx context.Context, id int64) error
	ListDeletedMedia(ctx context.Context) ([]model.Media, error)
}

// TagRepo manages tags and their assignment to media.
type TagRepo interface {
	CreateTag(ctx context.Context, name string) (int64, error)
	GetTagByID(ctx context.Context, id int64) (*model.Tag, error)
	GetTagByName(ctx context.Context, name string) (*model.Tag, error)
	ListTags(ctx context.Context) ([]model.Tag, error)
	DeleteTag(ctx context.Context, id int64) error
	AssignTag(ctx context.Context, mediaID, tagID int64) error
	RemoveTag(ctx context.Context, mediaID, tagID int64) error
	ListTagsByMedia(ctx context.Context, mediaID int64) ([]model.Tag, error)
}

// FavoriteRepo manages user favorites.
type FavoriteRepo interface {
	ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error)
	IsFavorite(ctx context.Context, userID, mediaID int64) (bool, error)
	ListFavoritesByUser(ctx context.Context, userID int64) ([]model.Favorite, error)
}

// PlaybackProgressRepo manages resume positions.
type PlaybackProgressRepo interface {
	UpsertProgress(ctx context.Context, progress *model.PlaybackProgress) error
	GetProgress(ctx context.Context, userID, mediaID int64) (*model.PlaybackProgress, error)
	ListProgressByUser(ctx context.Context, userID int64) ([]model.PlaybackProgress, error)
}

// PlaybackAccumulatorRepo manages the 60s playback counter rule.
type PlaybackAccumulatorRepo interface {
	UpsertAccumulator(ctx context.Context, acc *model.PlaybackAccumulator) error
	GetAccumulator(ctx context.Context, sessionID string, mediaID int64) (*model.PlaybackAccumulator, error)
}

// SessionRepo manages browser sessions.
type SessionRepo interface {
	CreateSession(ctx context.Context, session *model.Session) error
	GetSessionByID(ctx context.Context, id string) (*model.Session, error)
	DeleteSession(ctx context.Context, id string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error
}

// ShareRepo manages public share links.
type ShareRepo interface {
	CreateShare(ctx context.Context, share *model.Share) error
	GetShareByToken(ctx context.Context, token string) (*model.Share, error)
	ListSharesByMedia(ctx context.Context, mediaID int64) ([]model.Share, error)
	UseShare(ctx context.Context, token string) error
	DeleteShare(ctx context.Context, token string) error
	DeleteExpiredShares(ctx context.Context, now time.Time) error
}

// NoteRepo manages per-user, per-media notes.
type NoteRepo interface {
	UpsertNote(ctx context.Context, note *model.Note) error
	GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error)
	DeleteNote(ctx context.Context, mediaID, userID int64) error
}
