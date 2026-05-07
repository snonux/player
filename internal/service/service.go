// Package service contains application business logic.
package service

import (
	"context"
	"errors"
	"io"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

// Sentinel errors returned by the service layer.
var (
	ErrNotFound             = errors.New("not found")
	ErrForbidden            = errors.New("access denied")
	ErrShareNotFound        = errors.New("share not found")
	ErrShareExpired         = errors.New("share expired")
	ErrMediaNotFound        = errors.New("media not found")
	ErrUnsupportedExtension = errors.New("unsupported file extension")
	ErrAlreadyBootstrapped  = errors.New("already bootstrapped")
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrInvalidFeed          = errors.New("invalid feed")
)

// MediaQueryFilter defines query parameters for listing media from the API layer.
// It mirrors repository.MediaFilter but lives in the service layer to avoid coupling.
type MediaQueryFilter struct {
	SetID       *int64           // SetID restricts results to one set.
	SetIDs      []int64          // SetIDs restricts results to multiple selected sets.
	Type        *model.MediaType // Type restricts results to one media type.
	Search      string           // Search filters by filename or relative path.
	Tags        []string         // Tags restricts results to media with all listed tags.
	Favorites   bool             // Favorites restricts results to the current user's favorites.
	MinDuration *float64         // MinDuration is the minimum duration in seconds.
	MaxDuration *float64         // MaxDuration is the maximum duration in seconds.
	MinFileSize *int64           // MinFileSize is the minimum file size in bytes.
	MaxFileSize *int64           // MaxFileSize is the maximum file size in bytes.
	Sort        string           // Sort chooses the order: name, date, duration, play_count, or random.
	Limit       int              // Limit caps the number of returned rows.
	Offset      int              // Offset skips rows before returning results.
}

// MediaBrowseService handles read-only browsing and media streaming operations.
type MediaBrowseService interface {
	// ListSets returns the sets visible to a user.
	ListSets(ctx context.Context, userID int64) ([]model.Set, error)
	// GetMediaDetail returns media metadata and user-specific related state.
	GetMediaDetail(ctx context.Context, mediaID, userID int64) (*MediaDetail, error)
	// ListMedia returns media visible to a user for the given filter.
	ListMedia(ctx context.Context, userID int64, filter MediaQueryFilter) ([]model.Media, error)
	// StreamMedia returns a playable file for an authorized user.
	StreamMedia(ctx context.Context, mediaID, userID int64) (*FileResult, error)
	// DownloadMedia returns a downloadable file for an authorized user.
	DownloadMedia(ctx context.Context, mediaID, userID int64) (*FileResult, error)
	// GetThumbnail returns a media thumbnail for an authorized user.
	GetThumbnail(ctx context.Context, mediaID, userID int64) (*FileResult, error)
	// BrowseSet returns folders and media below a set path.
	BrowseSet(ctx context.Context, setID, userID int64, parent string) (*BrowseResult, error)
	// GetSetCover returns the cover image for a set folder.
	GetSetCover(ctx context.Context, setID int64, folder string, userID int64) (*FileResult, error)
}

// MediaWriteService handles mutations such as upload, soft-delete and restore.
type MediaWriteService interface {
	// SoftDeleteMedia marks a media item deleted for an authorized user.
	SoftDeleteMedia(ctx context.Context, mediaID, userID int64) error
	// RestoreMedia restores a soft-deleted media item for an authorized user.
	RestoreMedia(ctx context.Context, mediaID, userID int64) error
	// UploadMedia stores an uploaded media file in a set.
	UploadMedia(ctx context.Context, setID, userID int64, filename string, data io.Reader, size int64) (*model.Media, error)
	// RegenerateThumbnail refreshes a media item's thumbnail.
	RegenerateThumbnail(ctx context.Context, mediaID, userID int64) error
	// RegenerateSetCover refreshes a folder cover image for a set.
	RegenerateSetCover(ctx context.Context, setID int64, folder string, userID int64) error
}

// BrowseFolder is a named folder within a set's directory tree.
type BrowseFolder struct {
	Name     string `json:"name"`
	HasCover bool   `json:"has_cover"`
}

// BrowseResult is the content of one directory inside a set.
type BrowseResult struct {
	CurrentPath string                           `json:"current_path"`
	Folders     []BrowseFolder                   `json:"folders"`
	Media       []model.Media                    `json:"media"`
	Episodes    []model.PodcastEpisodeWithStatus `json:"episodes,omitempty"`
}

// SharedMediaView exposes only the metadata fields needed for a public share page.
type SharedMediaView struct {
	ID            int64           `json:"id"`
	FileName      string          `json:"file_name"`
	Type          model.MediaType `json:"type"`
	Duration      float64         `json:"duration"`
	Codec         string          `json:"codec"`
	Resolution    string          `json:"resolution"`
	Bitrate       int             `json:"bitrate"`
	FileSizeBytes int64           `json:"file_size_bytes"`
}

// GetSharedMediaResult wraps media metadata needed to render a share page.
type GetSharedMediaResult struct {
	Media       *SharedMediaView `json:"media"`
	HasThumb    bool             `json:"has_thumb"`
	StreamURL   string           `json:"stream_url"`
	DownloadURL string           `json:"download_url"`
	ThumbURL    string           `json:"thumb_url,omitempty"`
}

// ShareInfo augments a share with its associated media metadata.
type ShareInfo struct {
	Token     string          `json:"token"`
	MediaID   int64           `json:"media_id"`
	FileName  string          `json:"file_name"`
	MediaType model.MediaType `json:"media_type"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt time.Time       `json:"expires_at"`
	MaxUses   *int            `json:"max_uses,omitempty"`
	UsedCount int             `json:"used_count"`
}

// MediaShareService handles creation, validation and revocation of share links.
type MediaShareService interface {
	// CreateShare creates a public share link for a media item.
	CreateShare(ctx context.Context, userID, mediaID int64, expiresAt time.Time) (*model.Share, error)
	// ListShares returns shares for a media item visible to a user.
	ListShares(ctx context.Context, mediaID, userID int64) ([]model.Share, error)
	// RevokeShare removes a share link owned by a user.
	RevokeShare(ctx context.Context, token string, userID int64) error
	// ValidateShareToken returns a usable share for a token.
	ValidateShareToken(ctx context.Context, token string) (*model.Share, error)
	// StreamSharedMedia returns a playable file for a share token.
	StreamSharedMedia(ctx context.Context, token string) (*FileResult, error)
	// GetSharedMedia returns public metadata and URLs for a share token.
	GetSharedMedia(ctx context.Context, token string) (*GetSharedMediaResult, error)
	// GetSharedThumbnail returns a thumbnail for a share token.
	GetSharedThumbnail(ctx context.Context, token string) (*FileResult, error)
	// ListMyShares returns all shares created by a user.
	ListMyShares(ctx context.Context, userID int64) ([]ShareInfo, error)
}

// MediaTagService handles tagging of media items.
type MediaTagService interface {
	// AssignTag attaches a named tag to a media item.
	AssignTag(ctx context.Context, mediaID, userID int64, tagName string) error
	// RemoveTag detaches a named tag from a media item.
	RemoveTag(ctx context.Context, mediaID, userID int64, tagName string) error
}

// MediaFavoriteService handles toggling favorite status.
type MediaFavoriteService interface {
	// ToggleFavorite flips and returns the user's favorite state for a media item.
	ToggleFavorite(ctx context.Context, userID, mediaID int64) (bool, error)
}

// MediaNoteService handles CRUD for per-user per-media notes.
type MediaNoteService interface {
	// GetNote returns a user's note for a media item.
	GetNote(ctx context.Context, mediaID, userID int64) (*model.Note, error)
	// UpsertNote creates or updates a user's note.
	UpsertNote(ctx context.Context, note *model.Note) error
	// DeleteNote removes a user's note for a media item.
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
	// ListTrash returns soft-deleted media.
	ListTrash(ctx context.Context) ([]model.Media, error)
	// TriggerRescan starts a media library scan.
	TriggerRescan(ctx context.Context) error
	// ScanProgress returns the current or last scan state.
	ScanProgress(ctx context.Context) model.ScanProgress
	// ListUsers returns all application users.
	ListUsers(ctx context.Context) ([]model.User, error)
	// CreateUser creates a user account.
	CreateUser(ctx context.Context, username, password string, isAdmin bool) (*model.User, error)
	// DeleteUser removes a user account.
	DeleteUser(ctx context.Context, id int64) error
	// ListPermissions returns the set permission matrix.
	ListPermissions(ctx context.Context) (*PermissionsMatrix, error)
	// GrantPermission grants a user access to a set.
	GrantPermission(ctx context.Context, setID, userID int64, role model.Role) error
	// RevokePermission removes a user's access to a set.
	RevokePermission(ctx context.Context, setID, userID int64) error
}

// AuthService handles bootstrap, login, and user lookups for middleware.
type AuthService interface {
	// Bootstrap creates the first admin account and session.
	Bootstrap(ctx context.Context, username, password string) (*AuthResult, error)
	// Login authenticates a user and creates a session.
	Login(ctx context.Context, username, password string) (*AuthResult, error)
	// CountUsers returns the number of user accounts.
	CountUsers(ctx context.Context) (int, error)
	// GetUserByID returns a user by database ID.
	GetUserByID(ctx context.Context, id int64) (*model.User, error)
}

// AuthResult contains the authenticated user and session ID.
type AuthResult struct {
	User      *model.User
	SessionID string
}

// ProgressService handles playback progress updates.
type ProgressService interface {
	// UpdateProgress stores a playback position and updates play-count accounting.
	UpdateProgress(ctx context.Context, sessionID string, userID, mediaID int64, position float64) error
}

// MediaStreamer prepares authorized file results for HTTP streaming.
type MediaStreamer interface {
	// Open opens a file result and returns the headers/reader needed by the API.
	Open(ctx context.Context, file *FileResult, attachment bool) (*StreamResult, error)
	// Remux writes a remuxed stream to w for results where StreamResult.Remuxed is true.
	Remux(ctx context.Context, stream *StreamResult, w io.Writer) error
}

// PermissionsMatrix is the shape returned by ListPermissions.
type PermissionsMatrix struct {
	Sets        []model.Set           `json:"sets"`
	Users       []model.User          `json:"users"`
	Permissions []model.SetPermission `json:"permissions"`
}

// FileResult contains info for serving a file.
type FileResult struct {
	Path     string
	FileName string
	FileSize int64
	Duration float64 // DB-stored duration (seconds), used for remuxed streams.
}

// StreamResult contains an opened file and metadata needed for HTTP streaming.
type StreamResult struct {
	File        io.ReadSeekCloser
	Path        string
	FileName    string
	Size        int64
	ModTime     time.Time
	ContentType string
	Attachment  bool
	Remuxed     bool
	Duration    float64
}

// MediaDetail combines media with related data.
type MediaDetail struct {
	Media    *model.Media            `json:"media"`
	Tags     []model.Tag             `json:"tags"`
	Favorite bool                    `json:"favorite"`
	Note     *model.Note             `json:"note,omitempty"`
	Progress *model.PlaybackProgress `json:"progress,omitempty"`
}

// ResumeFrom returns the saved playback position in seconds, or 0 if none.
func (d *MediaDetail) ResumeFrom() float64 {
	if d.Progress != nil {
		return d.Progress.PositionSeconds
	}
	return 0
}
