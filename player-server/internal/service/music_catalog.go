package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"codeberg.org/snonux/player/internal/model"
)

// Album is a caller-visible aggregate over existing media identities.
type Album struct {
	Key          string        `json:"album_key"`
	Title        string        `json:"title"`
	Artist       string        `json:"artist"`
	ArtistKey    string        `json:"artist_key"`
	Year         int           `json:"year,omitempty"`
	FromFolder   bool          `json:"from_folder"`
	CoverMediaID int64         `json:"cover_media_id,omitempty"`
	SongCount    int           `json:"song_count"`
	Duration     float64       `json:"duration"`
	Songs        []model.Media `json:"songs,omitempty"`
}

// CatalogSource exposes only permission-aware reads needed by the music lens.
type CatalogSource interface {
	ListSets(context.Context, int64) ([]model.Set, error)
	ListMedia(context.Context, int64, MediaQueryFilter) ([]model.Media, error)
}

var releaseIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var spokenSourcePattern = regexp.MustCompile(`(?i)(^|[^a-z])audio[\s_-]*books?([^a-z]|$)`)

// MusicAlbums builds aggregates only after the source has applied caller ACLs.
func MusicAlbums(ctx context.Context, source CatalogSource, userID int64) ([]Album, error) {
	sets, err := source.ListSets(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("music sets: %w", err)
	}
	permitted := map[int64]model.Set{}
	for _, set := range sets {
		if !set.IsPodcast {
			permitted[set.ID] = set
		}
	}
	audioType := model.MediaTypeAudio
	media, err := source.ListMedia(ctx, userID, MediaQueryFilter{Type: &audioType})
	if err != nil {
		return nil, fmt.Errorf("music songs: %w", err)
	}
	groups := map[string]*Album{}
	for _, song := range media {
		set, ok := permitted[song.SetID]
		if !ok || song.Type != model.MediaTypeAudio || song.DeletedAt != nil || strings.EqualFold(path.Ext(song.FileName), ".m4b") || spokenSourcePattern.MatchString(set.Name+" "+song.RelPath+" "+song.FileName) {
			continue
		}
		album := albumForSong(song, set)
		group := groups[album.Key]
		if group == nil {
			group = &album
			groups[album.Key] = group
		}
		group.Songs = append(group.Songs, song)
	}
	result := make([]Album, 0, len(groups))
	for _, album := range groups {
		sort.Slice(album.Songs, func(i, j int) bool { return albumSongLess(album.Songs[i], album.Songs[j]) })
		album.SongCount = len(album.Songs)
		for _, song := range album.Songs {
			album.Duration += song.Duration
			if album.CoverMediaID == 0 && song.ThumbnailPath != "" {
				album.CoverMediaID = song.ID
			}
		}
		result = append(result, *album)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if normalized(a.Artist) != normalized(b.Artist) {
			return normalized(a.Artist) < normalized(b.Artist)
		}
		if normalized(a.Title) != normalized(b.Title) {
			return normalized(a.Title) < normalized(b.Title)
		}
		return a.Key < b.Key
	})
	return result, nil
}

func albumForSong(song model.Media, set model.Set) Album {
	m := song.Music
	if m == nil {
		m = &model.MusicMetadata{}
	}
	title, artist := strings.TrimSpace(m.Album), strings.TrimSpace(m.AlbumArtist)
	if artist == "" {
		artist = strings.TrimSpace(m.Artist)
	}
	if artist == "" {
		artist = "Unknown artist"
	}
	identity := normalized(artist) + "\x00" + normalized(title)
	fromFolder := title == ""
	if fromFolder {
		folder := path.Dir(song.RelPath)
		title = path.Base(folder)
		if folder == "." || folder == "" {
			title = set.Name
		}
		identity = fmt.Sprintf("folder:%d:%s", song.SetID, folder)
	}
	if releaseIDPattern.MatchString(strings.TrimSpace(m.ReleaseMBID)) {
		identity = "mbid:" + strings.ToLower(strings.TrimSpace(m.ReleaseMBID))
	}
	return Album{Key: catalogKey("al", identity), Title: title, Artist: artist, ArtistKey: catalogKey("ar", normalized(artist)), Year: m.Year, FromFolder: fromFolder}
}

func catalogKey(prefix, value string) string {
	return fmt.Sprintf("%s_%x", prefix, sha256.Sum256([]byte(value)))
}
func normalized(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
func albumSongLess(a, b model.Media) bool {
	number := func(m *model.MusicMetadata, disc bool) int {
		if m == nil {
			return int(^uint(0) >> 1)
		}
		n := m.Track
		if disc {
			n = m.Disc
		}
		if n <= 0 {
			return int(^uint(0) >> 1)
		}
		return n
	}
	for _, disc := range []bool{true, false} {
		x, y := number(a.Music, disc), number(b.Music, disc)
		if x != y {
			return x < y
		}
	}
	if a.FileName != b.FileName {
		return a.FileName < b.FileName
	}
	if a.RelPath != b.RelPath {
		return a.RelPath < b.RelPath
	}
	return a.ID < b.ID
}
