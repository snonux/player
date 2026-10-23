package service

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/subsonic"
)

// SubsonicService exposes ACL-filtered music catalog data for Subsonic clients.
type SubsonicService struct {
	source CatalogSource
}

// NewSubsonicService builds a Subsonic catalog adapter over MusicAlbums.
func NewSubsonicService(source CatalogSource) *SubsonicService {
	return &SubsonicService{source: source}
}

// MusicFolders returns permitted non-podcast sets as Subsonic music folders.
func (s *SubsonicService) MusicFolders(ctx context.Context, userID int64) ([]subsonic.MusicFolder, error) {
	sets, err := s.source.ListSets(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("subsonic music folders: %w", err)
	}
	out := make([]subsonic.MusicFolder, 0, len(sets))
	for _, set := range sets {
		if set.IsPodcast {
			continue
		}
		out = append(out, subsonic.MusicFolder{
			ID:   strconv.FormatInt(set.ID, 10),
			Name: set.Name,
		})
	}
	sort.Slice(out, func(i, j int) bool { return normalized(out[i].Name) < normalized(out[j].Name) })
	return out, nil
}

// ArtistIndex builds the letter-grouped artist index from authorized albums.
func (s *SubsonicService) ArtistIndex(ctx context.Context, userID int64, folderID string) ([]subsonic.Index, error) {
	albums, err := s.albums(ctx, userID, folderID)
	if err != nil {
		return nil, err
	}
	people := MusicArtists(albums)
	groups := map[string][]subsonic.Artist{}
	for _, person := range people {
		if !containsRole(person.Roles, "track_artist") && !containsRole(person.Roles, "album_artist") {
			continue
		}
		letter := indexLetter(person.Name)
		groups[letter] = append(groups[letter], subsonic.Artist{
			ID:         person.Key,
			Name:       person.Name,
			AlbumCount: person.AlbumCount,
		})
	}
	letters := make([]string, 0, len(groups))
	for letter := range groups {
		letters = append(letters, letter)
	}
	sort.Strings(letters)
	out := make([]subsonic.Index, 0, len(letters))
	for _, letter := range letters {
		artists := groups[letter]
		sort.Slice(artists, func(i, j int) bool { return normalized(artists[i].Name) < normalized(artists[j].Name) })
		out = append(out, subsonic.Index{Name: letter, Artist: artists})
	}
	return out, nil
}

// ArtistDetail returns one artist and their albums, or nil when missing/denied.
func (s *SubsonicService) ArtistDetail(ctx context.Context, userID int64, artistID, folderID string) (*subsonic.Artist, error) {
	albums, err := s.albums(ctx, userID, folderID)
	if err != nil {
		return nil, err
	}
	for _, person := range MusicArtists(albums) {
		if person.Key != artistID {
			continue
		}
		detail := &subsonic.Artist{
			ID:         person.Key,
			Name:       person.Name,
			AlbumCount: person.AlbumCount,
			Album:      make([]subsonic.Album, 0, len(person.Albums)),
		}
		for _, release := range person.Albums {
			detail.Album = append(detail.Album, mapAlbum(release.Album, false))
		}
		return detail, nil
	}
	return nil, nil
}

// AlbumDetail returns one album with songs, or nil when missing/denied.
func (s *SubsonicService) AlbumDetail(ctx context.Context, userID int64, albumID, folderID string) (*subsonic.Album, error) {
	albums, err := s.albums(ctx, userID, folderID)
	if err != nil {
		return nil, err
	}
	for _, album := range albums {
		if album.Key != albumID {
			continue
		}
		mapped := mapAlbum(album, true)
		return &mapped, nil
	}
	return nil, nil
}

// SongDetail returns one song by media id, or nil when missing/denied.
func (s *SubsonicService) SongDetail(ctx context.Context, userID int64, songID, folderID string) (*subsonic.Song, error) {
	id, err := strconv.ParseInt(songID, 10, 64)
	if err != nil || id <= 0 {
		return nil, nil
	}
	albums, err := s.albums(ctx, userID, folderID)
	if err != nil {
		return nil, err
	}
	for _, album := range albums {
		for _, song := range album.Songs {
			if song.ID == id {
				mapped := mapSong(song, album)
				return &mapped, nil
			}
		}
	}
	return nil, nil
}

// Search runs metadata search over the caller's permitted catalog.
func (s *SubsonicService) Search(ctx context.Context, userID int64, query, folderID string, artistCount, artistOffset, albumCount, albumOffset, songCount, songOffset int) (subsonic.SearchResult3, error) {
	albums, err := s.albums(ctx, userID, folderID)
	if err != nil {
		return subsonic.SearchResult3{}, err
	}
	// Empty query returns the full catalog slice (OpenSubsonic offline sync).
	result := SearchMusic(albums, query, "", "")
	if query == "" {
		result = MusicSearchResult{
			Songs:   flattenSongs(albums),
			Albums:  stripAlbumSongs(albums),
			Artists: searchableArtists(albums),
		}
	}
	out := subsonic.SearchResult3{
		Artist: pageArtists(result.Artists, artistOffset, artistCount),
		Album:  pageAlbums(result.Albums, albumOffset, albumCount),
		Song:   pageSongs(result.Songs, albums, songOffset, songCount),
	}
	return out, nil
}

func (s *SubsonicService) albums(ctx context.Context, userID int64, folderID string) ([]Album, error) {
	albums, err := MusicAlbums(ctx, s.source, userID)
	if err != nil {
		return nil, fmt.Errorf("subsonic albums: %w", err)
	}
	if folderID == "" {
		return albums, nil
	}
	setID, err := strconv.ParseInt(folderID, 10, 64)
	if err != nil || setID <= 0 {
		return nil, nil
	}
	filtered := make([]Album, 0, len(albums))
	for _, album := range albums {
		songs := make([]model.Media, 0, len(album.Songs))
		for _, song := range album.Songs {
			if song.SetID == setID {
				songs = append(songs, song)
			}
		}
		if len(songs) == 0 {
			continue
		}
		album.Songs = songs
		album.SongCount = len(songs)
		album.Duration = 0
		for _, song := range songs {
			album.Duration += song.Duration
		}
		filtered = append(filtered, album)
	}
	return filtered, nil
}

func mapAlbum(album Album, withSongs bool) subsonic.Album {
	out := subsonic.Album{
		ID:        album.Key,
		Name:      album.Title,
		Artist:    album.Artist,
		ArtistID:  album.ArtistKey,
		SongCount: album.SongCount,
		Duration:  int(album.Duration + 0.5),
		Year:      album.Year,
	}
	if !album.AddedAt.IsZero() {
		out.Created = album.AddedAt.UTC().Format(time.RFC3339)
	}
	if album.CoverMediaID > 0 {
		out.CoverArt = strconv.FormatInt(album.CoverMediaID, 10)
	}
	if withSongs {
		out.Song = make([]subsonic.Song, 0, len(album.Songs))
		for _, song := range album.Songs {
			out.Song = append(out.Song, mapSong(song, album))
		}
	}
	return out
}

func mapSong(song model.Media, album Album) subsonic.Song {
	title := strings.TrimSuffix(song.FileName, path.Ext(song.FileName))
	artist, year, track, disc, genre := "", 0, 0, 0, ""
	if song.Music != nil {
		if strings.TrimSpace(song.Music.Title) != "" {
			title = song.Music.Title
		}
		artist = strings.TrimSpace(song.Music.Artist)
		year = song.Music.Year
		track = song.Music.Track
		disc = song.Music.Disc
		if len(song.Music.Genres) > 0 {
			genre = song.Music.Genres[0]
		}
	}
	if artist == "" {
		artist = album.Artist
	}
	suffix := strings.TrimPrefix(strings.ToLower(path.Ext(song.FileName)), ".")
	out := subsonic.Song{
		ID:          strconv.FormatInt(song.ID, 10),
		Parent:      album.Key,
		IsDir:       false,
		Title:       title,
		Album:       album.Title,
		Artist:      artist,
		Track:       track,
		Year:        year,
		Genre:       genre,
		Size:        song.FileSizeBytes,
		ContentType: audioContentType(suffix),
		Suffix:      suffix,
		Duration:    int(song.Duration + 0.5),
		BitRate:     song.Bitrate / 1000,
		Path:        song.RelPath,
		IsVideo:     false,
		Created:     song.CreatedAt.UTC().Format(time.RFC3339),
		AlbumID:     album.Key,
		ArtistID:    album.ArtistKey,
		Type:        "music",
		DiscNumber:  disc,
	}
	if song.ThumbnailPath != "" {
		out.CoverArt = strconv.FormatInt(song.ID, 10)
	} else if album.CoverMediaID > 0 {
		out.CoverArt = strconv.FormatInt(album.CoverMediaID, 10)
	}
	return out
}

func audioContentType(suffix string) string {
	switch suffix {
	case "mp3":
		return "audio/mpeg"
	case "flac":
		return "audio/flac"
	case "ogg", "oga":
		return "audio/ogg"
	case "m4a", "aac":
		return "audio/mp4"
	case "wav":
		return "audio/wav"
	case "opus":
		return "audio/opus"
	default:
		if suffix == "" {
			return "application/octet-stream"
		}
		return "audio/" + suffix
	}
}

func indexLetter(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "#"
	}
	r := []rune(strings.ToUpper(name))[0]
	if unicode.IsLetter(r) {
		return string(r)
	}
	return "#"
}

func flattenSongs(albums []Album) []model.Media {
	var songs []model.Media
	for _, album := range albums {
		songs = append(songs, album.Songs...)
	}
	return songs
}

func stripAlbumSongs(albums []Album) []Album {
	out := make([]Album, 0, len(albums))
	for _, album := range albums {
		album.Songs, album.Discs, album.SongCredits = nil, nil, nil
		out = append(out, album)
	}
	return out
}

func searchableArtists(albums []Album) []Artist {
	var people []Artist
	for _, person := range MusicArtists(albums) {
		if containsRole(person.Roles, "track_artist") || containsRole(person.Roles, "album_artist") {
			person.Albums = nil
			people = append(people, person)
		}
	}
	return people
}

func pageArtists(people []Artist, offset, count int) []subsonic.Artist {
	if count < 0 {
		count = 0
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(people) {
		return []subsonic.Artist{}
	}
	end := offset + count
	if end > len(people) || count == 0 {
		end = len(people)
	}
	if count == 0 {
		return []subsonic.Artist{}
	}
	out := make([]subsonic.Artist, 0, end-offset)
	for _, person := range people[offset:end] {
		out = append(out, subsonic.Artist{ID: person.Key, Name: person.Name, AlbumCount: person.AlbumCount})
	}
	return out
}

func pageAlbums(albums []Album, offset, count int) []subsonic.Album {
	if count <= 0 {
		return []subsonic.Album{}
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(albums) {
		return []subsonic.Album{}
	}
	end := offset + count
	if end > len(albums) {
		end = len(albums)
	}
	out := make([]subsonic.Album, 0, end-offset)
	for _, album := range albums[offset:end] {
		out = append(out, mapAlbum(album, false))
	}
	return out
}

func pageSongs(songs []model.Media, albums []Album, offset, count int) []subsonic.Song {
	if count <= 0 {
		return []subsonic.Song{}
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(songs) {
		return []subsonic.Song{}
	}
	byID := map[int64]Album{}
	for _, album := range albums {
		for _, song := range album.Songs {
			byID[song.ID] = album
		}
	}
	end := offset + count
	if end > len(songs) {
		end = len(songs)
	}
	out := make([]subsonic.Song, 0, end-offset)
	for _, song := range songs[offset:end] {
		out = append(out, mapSong(song, byID[song.ID]))
	}
	return out
}
