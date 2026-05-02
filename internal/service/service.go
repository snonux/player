// Package service contains application business logic.
package service

import (
	"context"
	"io"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// MediaBrowseService handles read-only browsing and media streaming operations.
type MediaBrowseService interface {
	ListSets(ctx context.Context, userID int64) ([]model.Set, error)
	GetMediaDetail(ctx context.Context, mediaID, userID int64) (*MediaDetail, error)
	ListMedia(ctx context.Context, userID int64, filter repository.MediaFilter) ([]model.Media, error)
	StreamMedia(ctx context.Context, mediaID, userID int64) (*FileResult, error)
	DownloadMedia(ctx context.Context, mediaID, userID int64) (*FileResult, error)
	GetThumbnail(ctx context.Context, mediaID, userID int64) (*FileResult, error)
	RegenerateThumbnail(ctx context.Context, mediaID, userID int64) error
	RegenerateSetCover(ctx context.Context, setID, userID int64) error
}

// MediaWriteService handles mutations such as upload, soft-delete and restore.
type MediaWriteService interface {
	SoftDeleteMedia(ctx context.Context, mediaID, userID int64) error
	RestoreMedia(ctx context.Context, mediaID, userID int64) error
	UploadMedia(ctx context.Context, setID, userID int64, filename string, data io.Reader, size int64) (*model.Media, error)
}

// GetSharedMediaResult wraps media metadata needed to render a share page.
	type GetSharedMediaResult struct {
		Media      *model.Media `json:"media"`
		HasThumb   bool         `json:"has_thumb"`
		StreamURL  string       `json:"stream_url"`
		ThumbURL   string       `json:"thumb_url"`
	}

// MediaShareService handles creation, validation and revocation of share links.
	type MediaShareService interface {
		CreateShare(ctx context.Context, userID, mediaID int64, expiresAt time.Time) (*model.Share, error)
		ListShares(ctx context.Context, mediaID, userID int64) ([]model.Share, error)
		RevokeShare(ctx context.Context, token string, userID int64) error
		ValidateShareToken(ctx context.Context, token string) (*model.Share, error)
		StreamSharedMedia(ctx context.Context, token string) (*FileResult, error)
		GetSharedMedia(ctx context.Context, token string) (*GetSharedMediaResult, error)
	}

// MediaTagService handles tagging of media items.
type MediaTagService interface {
	AssignTag(ctx context.Context, mediaID, userID int64, tagName string) error
	RemoveTag(ctx context.Context, mediaID, userID int64, tagName string) error
}

// MediaFavoriteService handles toggling favorite status.
type MediaFavoriteService interface {
	ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error)
}

// MediaNoteService handles CRUD for per-user per-media notes.
type MediaNoteService interface {
	GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error)
	UpsertNote(ctx context.Context, note *model.Note) error
	DeleteNote(ctx context.Context, mediaID, userID int64) error
}

// MediaService is the composite interface combining all media-related roles.
type MediaService interface {
	MediaBrowseService
	MediaWriteService
	MediaShareService
	MediaTagService
	MediaFavoriteService
	MediaNoteService
}

// AdminService handles admin-only operations.
type AdminService interface {
	ListTrash(ctx context.Context) ([]model.Media, error)
	TriggerRescan(ctx context.Context) error
	ListUsers(ctx context.Context) ([]model.User, error)
	CreateUser(ctx context.Context, username, password string, isAdmin bool) (*model.User, error)
	DeleteUser(ctx context.Context, id int64) error
	ListPermissions(ctx context.Context) (*PermissionsMatrix, error)
	GrantPermission(ctx context.Context, setID, userID int64, role model.Role) error
	RevokePermission(ctx context.Context, setID, userID int64) error
}

// AuthService handles bootstrap and login operations.
type AuthService interface {
	Bootstrap(ctx context.Context, username, password string) (*AuthResult, error)
	Login(ctx context.Context, username, password string) (*AuthResult, error)
}

// AuthResult contains the authenticated user and session ID.
type AuthResult struct {
	User      *model.User
	SessionID string
}

// ProgressService handles playback progress updates.
type ProgressService interface {
	UpdateProgress(ctx context.Context, sessionID string, userID, mediaID int64, position float64) error
}

// PermissionsMatrix is the shape returned by ListPermissions.
type PermissionsMatrix struct {
	Sets        []model.Set         `json:"sets"`
	Users       []model.User        `json:"users"`
	Permissions []model.SetPermission `json:"permissions"`
}

// FileResult contains info for serving a file.
type FileResult struct {
	Path     string
	FileName string
	FileSize int64
}

// MediaDetail combines media with related data.
type MediaDetail struct {
	Media    *model.Media              `json:"media"`
	Tags     []model.Tag               `json:"tags"`
	Favorite bool                      `json:"favorite"`
	Note     *model.Note               `json:"note,omitempty"`
	Progress *model.PlaybackProgress  `json:"progress,omitempty"`
}

// ResumeFrom returns the saved playback position in seconds, or 0 if none.
func (d *MediaDetail) ResumeFrom() float64 {
	if d.Progress != nil {
		return d.Progress.PositionSeconds
	}
	return 0
}
