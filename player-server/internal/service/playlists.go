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

// List returns owner-scoped playlists with current visible totals and covers.
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

// Get rejects foreign documents before reading the catalog.
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
	name, err := validatePlaylist(name, comment)
	if err != nil {
		return nil, err
	}
	id, err := playlistID("pl")
	if err != nil {
		return nil, err
	}
	p := model.Playlist{ID: id, Name: name, Comment: comment}
	if err := s.repo.CreatePlaylist(ctx, user, p); err != nil {
		return nil, err
	}
	v := playlistView(p, nil)
	return &v, nil
}

// Update changes name and comment without replacing membership.
func (s *PlaylistService) Update(ctx context.Context, user int64, id, name, comment string) (*PlaylistView, error) {
	name, err := validatePlaylist(name, comment)
	if err != nil {
		return nil, err
	}
	ok, err := s.repo.UpdatePlaylist(ctx, user, id, name, comment)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	return s.Get(ctx, user, id)
}

// Delete removes only the caller's playlist; source music is unchanged.
func (s *PlaylistService) Delete(ctx context.Context, user int64, id string) error {
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
