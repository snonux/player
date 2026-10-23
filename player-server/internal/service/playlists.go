package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"unicode/utf8"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

// PlaylistView contains only currently accessible music and derived aggregates.
type PlaylistView struct {
	model.Playlist
	Songs        []model.Media `json:"songs"`
	SongCount    int           `json:"song_count"`
	Duration     float64       `json:"duration"`
	CoverMediaID int64         `json:"cover_media_id,omitempty"`
}

// PlaylistService combines durable account documents with permission-aware music.
type PlaylistService struct {
	repo   repository.PlaylistRepo
	source CatalogSource
}

// NewPlaylistService keeps storage and catalog dependencies independently testable.
func NewPlaylistService(repo repository.PlaylistRepo, source CatalogSource) *PlaylistService {
	return &PlaylistService{repo: repo, source: source}
}

// List returns readable playlists with current viewer-visible totals and covers.
func (s *PlaylistService) List(ctx context.Context, user int64) ([]PlaylistView, error) {
	docs, err := s.repo.ListPlaylists(ctx, user)
	if err != nil {
		return nil, err
	}
	songs, err := s.visibleSongs(ctx, user)
	if err != nil {
		return nil, err
	}
	result := make([]PlaylistView, 0, len(docs))
	for _, doc := range docs {
		p, err := s.repo.GetPlaylist(ctx, user, doc.ID)
		if err != nil {
			return nil, err
		}
		if p != nil {
			v := playlistView(*p, songs)
			v.Songs = []model.Media{}
			v.Entries = nil
			result = append(result, v)
		}
	}
	return result, nil
}

// Get rejects unreadable documents before reading the catalog.
func (s *PlaylistService) Get(ctx context.Context, user int64, id string) (*PlaylistView, error) {
	p, err := s.repo.GetPlaylist(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrNotFound
	}
	songs, err := s.visibleSongs(ctx, user)
	if err != nil {
		return nil, err
	}
	v := playlistView(*p, songs)
	return &v, nil
}

// Create validates human metadata and creates an empty document.
func (s *PlaylistService) Create(ctx context.Context, user int64, name, comment string) (*PlaylistView, error) {
	return s.CreateDetails(ctx, user, name, comment, nil)
}

// CreateDetails creates a private-by-default document with explicit optional visibility.
func (s *PlaylistService) CreateDetails(ctx context.Context, user int64, name, comment string, visibility *string) (*PlaylistView, error) {
	if visibility != nil && *visibility != "private" && *visibility != "shared" {
		return nil, &apiError{msg: "Visibility must be private or shared.", status: 400}
	}
	name, err := validatePlaylist(name, comment)
	if err != nil {
		return nil, err
	}
	id, err := playlistID("pl")
	if err != nil {
		return nil, err
	}
	p := model.Playlist{ID: id, Name: name, Comment: comment, Visibility: "private"}
	if visibility != nil {
		p.Visibility = *visibility
	}
	if err := s.repo.CreatePlaylist(ctx, user, p); err != nil {
		return nil, err
	}
	return s.Get(ctx, user, p.ID)
}

// Update changes name and comment without replacing membership.
func (s *PlaylistService) Update(ctx context.Context, user int64, id, name, comment string) (*PlaylistView, error) {
	return s.UpdateDetails(ctx, user, id, name, comment, nil, nil)
}

// UpdateDetails preserves omitted access fields and limits ownership transfers to administrators.
func (s *PlaylistService) UpdateDetails(ctx context.Context, user int64, id, name, comment string, visibility *string, owner *int64) (*PlaylistView, error) {
	p, err := s.Get(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if !p.CanEdit || (owner != nil && !p.CanTransfer) {
		return nil, ErrForbidden
	}
	name, err = validatePlaylist(name, comment)
	if err != nil {
		return nil, err
	}
	if visibility != nil && *visibility != "private" && *visibility != "shared" {
		return nil, &apiError{msg: "Visibility must be private or shared.", status: 400}
	}
	if owner != nil && *owner <= 0 {
		return nil, &apiError{msg: "Choose a valid owner.", status: 400}
	}
	ok, err := s.repo.UpdatePlaylistDetails(ctx, user, id, name, comment, visibility, owner)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &apiError{msg: "Playlist or selected owner is no longer available.", status: 400}
	}
	return s.Get(ctx, user, id)
}

// Delete removes an owner/admin-authorized playlist; source music is unchanged.
func (s *PlaylistService) Delete(ctx context.Context, user int64, id string) error {
	p, err := s.Get(ctx, user, id)
	if err != nil {
		return err
	}
	if !p.CanEdit {
		return ErrForbidden
	}
	ok, err := s.repo.DeletePlaylist(ctx, user, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// AddAlbum is M26's minimal population path. Later track editing is separate.
func (s *PlaylistService) AddAlbum(ctx context.Context, user int64, id, key string) (*PlaylistView, error) {
	p, err := s.repo.GetPlaylist(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrNotFound
	}
	if !p.CanEdit {
		return nil, ErrForbidden
	}
	albums, err := MusicAlbums(ctx, s.source, user)
	if err != nil {
		return nil, err
	}
	for _, a := range albums {
		if a.Key != key {
			continue
		}
		entries := make([]model.PlaylistEntry, 0, len(a.Songs))
		for _, song := range a.Songs {
			entry, err := playlistID("pe")
			if err != nil {
				return nil, err
			}
			entries = append(entries, model.PlaylistEntry{ID: entry, MediaID: song.ID})
		}
		ok, err := s.repo.AppendPlaylist(ctx, user, id, entries)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrNotFound
		}
		return s.Get(ctx, user, id)
	}
	return nil, ErrNotFound
}

// EditSongs validates all selected songs and occurrence identities before saving.
func (s *PlaylistService) EditSongs(ctx context.Context, user int64, id, action, entryID, beforeID string, mediaIDs []int64) (*PlaylistView, error) {
	p, err := s.Get(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if !p.CanEdit {
		return nil, ErrForbidden
	}
	change := repository.PlaylistChange{}
	switch action {
	case "add":
		if len(mediaIDs) == 0 || len(mediaIDs) > 1000 {
			return nil, &apiError{msg: "Select between 1 and 1000 songs.", status: 400}
		}
		visible, err := s.visibleSongs(ctx, user)
		if err != nil {
			return nil, err
		}
		for _, mediaID := range mediaIDs {
			if _, ok := visible[mediaID]; !ok {
				return nil, ErrNotFound
			}
			entry, err := playlistID("pe")
			if err != nil {
				return nil, err
			}
			change.Entries = append(change.Entries, model.PlaylistEntry{ID: entry, MediaID: mediaID})
		}
		// An absent undo anchor appends; a saved but inaccessible anchor is refused.
		if beforeID != "" {
			saved, err := s.repo.GetPlaylist(ctx, user, id)
			if err != nil {
				return nil, err
			}
			if saved == nil {
				return nil, ErrNotFound
			}
			for _, entry := range saved.Entries {
				if entry.ID == beforeID {
					if _, ok := visible[entry.MediaID]; !ok {
						return nil, ErrNotFound
					}
				}
			}
		}
		change.BeforeID = beforeID
	case "remove", "up", "down":
		at := -1
		for i, entry := range p.Entries {
			if entry.ID == entryID {
				at = i
				break
			}
		}
		if at < 0 {
			return nil, ErrNotFound
		}
		if action == "remove" {
			change.RemoveID = entryID
		} else {
			neighbor := at - 1
			if action == "down" {
				neighbor = at + 1
			}
			if neighbor < 0 || neighbor >= len(p.Entries) {
				return p, nil
			}
			change.MoveID = entryID
			change.NeighborID = p.Entries[neighbor].ID
		}
	default:
		return nil, &apiError{msg: "Unknown playlist song action.", status: 400}
	}
	ok, err := s.repo.EditPlaylist(ctx, user, id, change)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	return s.Get(ctx, user, id)
}

func (s *PlaylistService) visibleSongs(ctx context.Context, user int64) (map[int64]model.Media, error) {
	albums, err := MusicAlbums(ctx, s.source, user)
	if err != nil {
		return nil, err
	}
	songs := map[int64]model.Media{}
	for _, a := range albums {
		for _, song := range a.Songs {
			songs[song.ID] = song
		}
	}
	return songs, nil
}
func playlistView(p model.Playlist, songs map[int64]model.Media) PlaylistView {
	v := PlaylistView{Playlist: p, Songs: []model.Media{}}
	v.Entries = nil
	for _, entry := range p.Entries {
		song, ok := songs[entry.MediaID]
		if !ok {
			continue
		}
		v.Entries = append(v.Entries, entry)
		v.Songs = append(v.Songs, song)
		v.SongCount++
		v.Duration += song.Duration
		if v.CoverMediaID == 0 && song.ThumbnailPath != "" {
			v.CoverMediaID = song.ID
		}
	}
	return v
}
func validatePlaylist(name, comment string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || !utf8.ValidString(comment) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 200 || utf8.RuneCountInString(comment) > 2000 {
		return "", &apiError{msg: "Name must be 1–200 characters and comment at most 2000 characters.", status: 400}
	}
	return name, nil
}
func playlistID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("playlist identity: %w", err)
	}
	return fmt.Sprintf("%s_%x", prefix, b), nil
}
