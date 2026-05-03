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

// AccessHelperStore is the subset of Store required by service.accessHelper.
type AccessHelperStore interface {
	UserRepo
	MediaRepo
	SetRepo
	SetPermissionRepo
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

// BrowseServiceStore is the subset of Store required by service.BrowseService.
type BrowseServiceStore interface {
	UserRepo
	SetRepo
	SetPermissionRepo
	MediaRepo
	TagRepo
	FavoriteRepo
	PlaybackProgressRepo
	NoteRepo
}

// WriteServiceStore is the subset of Store required by service.WriteService.
type WriteServiceStore interface {
	UserRepo
	SetRepo
	SetPermissionRepo
	MediaRepo
}

// ShareServiceStore is the subset of Store required by service.ShareService.
type ShareServiceStore interface {
	MediaRepo
	ShareRepo
}

// TagServiceStore is the subset of Store required by service.TagService.
type TagServiceStore interface {
	MediaRepo
	TagRepo
}

// FavoriteServiceStore is the subset of Store required by service.FavService.
type FavoriteServiceStore interface {
	MediaRepo
	FavoriteRepo
}

// NoteServiceStore is the subset of Store required by service.NoteService.
type NoteServiceStore interface {
	MediaRepo
	NoteRepo
}

// TrashServiceStore is the subset of Store required by service.TrashService.
type TrashServiceStore interface {
	MediaRepo
}

// UserAdminServiceStore is the subset of Store required by service.UserAdminService.
type UserAdminServiceStore interface {
	UserRepo
}

// PermissionAdminServiceStore is the subset of Store required by service.PermissionAdminService.
type PermissionAdminServiceStore interface {
	UserRepo
	SetRepo
	SetPermissionRepo
}

// UserRepo manages application users.
type UserRepo interface {
	// CreateUser stores a new user and returns its database ID.
	CreateUser(ctx context.Context, user *model.User) (int64, error)
	// GetUserByID returns a user by database ID.
	GetUserByID(ctx context.Context, id int64) (*model.User, error)
	// GetUserByUsername returns a user by login name.
	GetUserByUsername(ctx context.Context, username string) (*model.User, error)
	// ListUsers returns all users.
	ListUsers(ctx context.Context) ([]model.User, error)
	// DeleteUser removes a user by database ID.
	DeleteUser(ctx context.Context, id int64) error
	// CountUsers returns the number of user accounts.
	CountUsers(ctx context.Context) (int, error)
}

// SetRepo manages media sets.
type SetRepo interface {
	// CreateSet stores a new media set and returns its database ID.
	CreateSet(ctx context.Context, set *model.Set) (int64, error)
	// GetSetByID returns a media set by database ID.
	GetSetByID(ctx context.Context, id int64) (*model.Set, error)
	// ListSets returns all media sets.
	ListSets(ctx context.Context) ([]model.Set, error)
	// UpdateSet replaces mutable fields for a media set.
	UpdateSet(ctx context.Context, set *model.Set) error
	// DeleteSet removes a media set by database ID.
	DeleteSet(ctx context.Context, id int64) error
}

// SetPermissionRepo manages set access grants.
type SetPermissionRepo interface {
	// GrantPermission creates or updates a user's access to a set.
	GrantPermission(ctx context.Context, perm *model.SetPermission) error
	// RevokePermission removes a user's access to a set.
	RevokePermission(ctx context.Context, setID, userID int64) error
	// GetPermission returns one user's permission for a set.
	GetPermission(ctx context.Context, setID, userID int64) (*model.SetPermission, error)
	// ListPermissionsBySet returns all permissions for a set.
	ListPermissionsBySet(ctx context.Context, setID int64) ([]model.SetPermission, error)
	// ListPermissionsByUser returns all set permissions for a user.
	ListPermissionsByUser(ctx context.Context, userID int64) ([]model.SetPermission, error)
}

// MediaFilter defines query parameters for listing media.
type MediaFilter struct {
	SetID         *int64           // SetID restricts results to one set.
	SetIDs        []int64          // SetIDs restricts results to multiple selected sets.
	AllowedSetIDs []int64          // AllowedSetIDs restricts results to sets visible to the user.
	Type          *model.MediaType // Type restricts results to one media type.
	Search        string           // Search filters by filename or relative path.
	Tags          []string         // Tags restricts results to media with all listed tags.
	Favorites     bool             // Favorites restricts results to the current user's favorites.
	UserID        int64            // UserID identifies the user for favorite and access filters.
	MinDuration   *float64         // MinDuration is the minimum duration in seconds.
	MaxDuration   *float64         // MaxDuration is the maximum duration in seconds.
	MinFileSize   *int64           // MinFileSize is the minimum file size in bytes.
	MaxFileSize   *int64           // MaxFileSize is the maximum file size in bytes.
	Sort          string           // Sort chooses the order: name, date, duration, play_count, or random.
	Limit         int              // Limit caps the number of returned rows.
	Offset        int              // Offset skips rows before returning results.
}

// MediaRepo manages media items.
type MediaRepo interface {
	// CreateMedia stores a new media item and returns its database ID.
	CreateMedia(ctx context.Context, media *model.Media) (int64, error)
	// UpdateMedia replaces mutable fields for a media item.
	UpdateMedia(ctx context.Context, media *model.Media) error
	// UpdateMediaThumbnail updates the thumbnail path for a media item.
	UpdateMediaThumbnail(ctx context.Context, id int64, thumbnailPath string) error
	// GetMediaByID returns a media item by database ID.
	GetMediaByID(ctx context.Context, id int64) (*model.Media, error)
	// ListMedia returns media matching the given filter.
	ListMedia(ctx context.Context, filter MediaFilter) ([]model.Media, error)
	// SoftDeleteMedia marks a media item deleted without removing its row.
	SoftDeleteMedia(ctx context.Context, id int64) error
	// RestoreMedia clears a media item's soft-delete marker.
	RestoreMedia(ctx context.Context, id int64) error
	// HardDeleteMedia permanently removes a media item.
	HardDeleteMedia(ctx context.Context, id int64) error
	// IncrementPlayCount increments the play counter for a media item.
	IncrementPlayCount(ctx context.Context, id int64) error
	// ListDeletedMedia returns soft-deleted media items.
	ListDeletedMedia(ctx context.Context) ([]model.Media, error)
}

// TagRepo manages tags and their assignment to media.
type TagRepo interface {
	// CreateTag stores a new tag and returns its database ID.
	CreateTag(ctx context.Context, name string) (int64, error)
	// GetTagByID returns a tag by database ID.
	GetTagByID(ctx context.Context, id int64) (*model.Tag, error)
	// GetTagByName returns a tag by name.
	GetTagByName(ctx context.Context, name string) (*model.Tag, error)
	// ListTags returns all tags.
	ListTags(ctx context.Context) ([]model.Tag, error)
	// DeleteTag removes a tag by database ID.
	DeleteTag(ctx context.Context, id int64) error
	// AssignTag attaches a tag to a media item.
	AssignTag(ctx context.Context, mediaID, tagID int64) error
	// RemoveTag detaches a tag from a media item.
	RemoveTag(ctx context.Context, mediaID, tagID int64) error
	// ListTagsByMedia returns all tags attached to a media item.
	ListTagsByMedia(ctx context.Context, mediaID int64) ([]model.Tag, error)
}

// FavoriteRepo manages user favorites.
type FavoriteRepo interface {
	// ToggleFavorite flips and returns a user's favorite state for a media item.
	ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error)
	// IsFavorite reports whether a user has favorited a media item.
	IsFavorite(ctx context.Context, userID, mediaID int64) (bool, error)
	// ListFavoritesByUser returns all favorites for a user.
	ListFavoritesByUser(ctx context.Context, userID int64) ([]model.Favorite, error)
}

// PlaybackProgressRepo manages resume positions.
type PlaybackProgressRepo interface {
	// UpsertProgress creates or updates a user's playback position.
	UpsertProgress(ctx context.Context, progress *model.PlaybackProgress) error
	// GetProgress returns a user's playback position for a media item.
	GetProgress(ctx context.Context, userID, mediaID int64) (*model.PlaybackProgress, error)
	// ListProgressByUser returns all saved playback positions for a user.
	ListProgressByUser(ctx context.Context, userID int64) ([]model.PlaybackProgress, error)
}

// PlaybackAccumulatorRepo manages the 60s playback counter rule.
type PlaybackAccumulatorRepo interface {
	// UpsertAccumulator creates or updates a playback accumulator.
	UpsertAccumulator(ctx context.Context, acc *model.PlaybackAccumulator) error
	// GetAccumulator returns the accumulator for a session and media item.
	GetAccumulator(ctx context.Context, sessionID string, mediaID int64) (*model.PlaybackAccumulator, error)
}

// SessionRepo manages browser sessions.
type SessionRepo interface {
	// CreateSession stores a browser session.
	CreateSession(ctx context.Context, session *model.Session) error
	// GetSessionByID returns a browser session by token.
	GetSessionByID(ctx context.Context, id string) (*model.Session, error)
	// DeleteSession removes a browser session by token.
	DeleteSession(ctx context.Context, id string) error
	// DeleteExpiredSessions removes sessions that expired before now.
	DeleteExpiredSessions(ctx context.Context, now time.Time) error
}

// ShareRepo manages public share links.
type ShareRepo interface {
	// CreateShare stores a public share link.
	CreateShare(ctx context.Context, share *model.Share) error
	// GetShareByToken returns a share link by token.
	GetShareByToken(ctx context.Context, token string) (*model.Share, error)
	// ListSharesByMedia returns all shares for a media item.
	ListSharesByMedia(ctx context.Context, mediaID int64) ([]model.Share, error)
	// ListSharesByUser returns all shares created by a user.
	ListSharesByUser(ctx context.Context, userID int64) ([]model.Share, error)
	// UseShare records one use of a share link.
	UseShare(ctx context.Context, token string) error
	// DeleteShare removes a share link by token.
	DeleteShare(ctx context.Context, token string) error
	// DeleteExpiredShares removes shares that expired before now.
	DeleteExpiredShares(ctx context.Context, now time.Time) error
}

// NoteRepo manages per-user, per-media notes.
type NoteRepo interface {
	// UpsertNote creates or updates a note.
	UpsertNote(ctx context.Context, note *model.Note) error
	// GetNote returns a user's note for a media item.
	GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error)
	// DeleteNote removes a user's note for a media item.
	DeleteNote(ctx context.Context, mediaID, userID int64) error
}
