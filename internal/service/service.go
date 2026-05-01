// Package service contains application business logic.
package service

import (
	"context"
	"io"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// MediaService handles media and set operations.
type MediaService interface {
	ListSets(ctx context.Context, userID int64) ([]model.Set, error)
	GetMediaDetail(ctx context.Context, mediaID, userID int64) (*MediaDetail, error)
	ListMedia(ctx context.Context, userID int64, filter repository.MediaFilter) ([]model.Media, error)
	StreamMedia(ctx context.Context, mediaID, userID int64) (*FileResult, error)
	DownloadMedia(ctx context.Context, mediaID, userID int64) (*FileResult, error)
	GetThumbnail(ctx context.Context, mediaID, userID int64) (*FileResult, error)
	RegenerateThumbnail(ctx context.Context, mediaID, userID int64) error
	RegenerateSetCover(ctx context.Context, setID, userID int64) error
	ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error)
	AssignTag(ctx context.Context, mediaID, userID int64, tagName string) error
	RemoveTag(ctx context.Context, mediaID, userID int64, tagName string) error
	SoftDeleteMedia(ctx context.Context, mediaID, userID int64) error
	RestoreMedia(ctx context.Context, mediaID, userID int64) error
	UploadMedia(ctx context.Context, setID, userID int64, filename string, data io.Reader, size int64) (*model.Media, error)
	CreateShare(ctx context.Context, userID, mediaID int64, expiresAt time.Time) (*model.Share, error)
	ListShares(ctx context.Context, mediaID, userID int64) ([]model.Share, error)
	RevokeShare(ctx context.Context, token string, userID int64) error
	ValidateShareToken(ctx context.Context, token string) (*model.Share, error)
	StreamSharedMedia(ctx context.Context, token string) (*FileResult, error)
	GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error)
	UpsertNote(ctx context.Context, note *model.Note) error
	DeleteNote(ctx context.Context, mediaID, userID int64) error
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
