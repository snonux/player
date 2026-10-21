package probe

import (
	"encoding/json"
	"strconv"
	"strings"

	"codeberg.org/snonux/player/internal/model"
)

// musicTags normalizes keys, never punctuation in human tag values.
func musicTags(tags map[string]string) map[string]string {
	normalized := make(map[string]string, len(tags))
	for key, value := range tags {
		normalized[strings.ToLower(key)] = strings.TrimSpace(value)
	}
	return normalized
}

// Container tags win over audio-stream tags; Vorbis/Opus usually use streams.
func mergeMusicTags(tags map[string]string, stream map[string]string) map[string]string {
	for key, value := range musicTags(stream) {
		if tags[key] == "" {
			tags[key] = value
		}
	}
	return tags
}

func parseMusicTags(tags map[string]string) *model.MusicMetadata {
	first := func(keys ...string) string {
		for _, key := range keys {
			if tags[key] != "" {
				return tags[key]
			}
		}
		return ""
	}
	m := &model.MusicMetadata{Title: first("title"), Album: first("album"), Date: first("date", "year")}
	m.ReleaseMBID = first("musicbrainz_albumid", "musicbrainz album id", "musicbrainz/release id")
	m.Artists = musicValues(first("artists", "artist"))
	m.Artist = first("artist")
	if m.Artist == "" || strings.ContainsRune(m.Artist, 0) {
		m.Artist = strings.Join(m.Artists, ", ")
	}
	m.AlbumArtists = musicValues(first("album_artists", "albumartists", "album_artist", "albumartist", "album artist"))
	m.AlbumArtist = strings.Join(m.AlbumArtists, ", ")
	m.Track, m.TrackTotal = musicNumber(first("track", "tracknumber"), first("tracktotal", "totaltracks"))
	m.Disc, m.DiscTotal = musicNumber(first("disc", "discnumber", "disk"), first("disctotal", "totaldiscs"))
	if len(m.Date) >= 4 {
		m.Year = positiveNumber(m.Date[:4])
		if m.Year > 9999 {
			m.Year = 0
		}
	}
	if m.Year == 0 {
		m.Year = positiveNumber(first("year"))
		if m.Year > 9999 {
			m.Year = 0
		}
	}
	m.Composers = musicValues(first("composers", "composer"))
	m.Performers = musicValues(first("performers", "performer"))
	m.Genres = musicValues(first("genre", "genres"))
	return m
}

// NUL is a structured separator. Slashes/commas/semicolons/feat. remain names.
func musicValues(value string) []string {
	var result []string
	for _, item := range strings.Split(value, "\x00") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		found := false
		for _, prior := range result {
			if prior == item {
				found = true
				break
			}
		}
		if !found {
			result = append(result, item)
		}
	}
	return result
}

func musicNumber(value, total string) (int, int) {
	parts := strings.Split(value, "/")
	number := positiveNumber(parts[0])
	count := positiveNumber(total)
	if len(parts) == 2 && count == 0 {
		count = positiveNumber(parts[1])
	}
	if len(parts) > 2 {
		return 0, count
	}
	return number, count
}

func positiveNumber(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 || n > 1000000 {
		return 0
	}
	return n
}

func outMusicTags(data []byte) map[string]string {
	var out ffprobeOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	tags := musicTags(out.Format.Tags)
	for _, stream := range out.Streams {
		if stream.CodecType == "audio" {
			tags = mergeMusicTags(tags, stream.Tags)
		}
	}
	return tags
}
