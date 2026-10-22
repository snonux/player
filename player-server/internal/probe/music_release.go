package probe

import (
	"sort"
	"strings"

	"codeberg.org/snonux/player/internal/model"
)

func parseReleaseTags(m *model.MusicMetadata, tags map[string]string) {
	first := func(keys ...string) string {
		for _, key := range keys {
			if tags[key] != "" {
				return tags[key]
			}
		}
		return ""
	}
	m.ReleaseDate = first("release_date", "releasedate", "date", "year")
	m.OriginalDate = first("original_date", "originaldate", "originalyear")
	m.RecordingDate = first("recording_date", "recordingdate")
	m.ReleaseVersion = first("release_version", "albumversion", "album_version")
	m.ReleaseTypes = musicValues(first("releasetype", "release_type", "musicbrainz_albumtype", "musicbrainz album type"))
	m.Labels = musicValues(first("label", "publisher", "organization"))
	m.CatalogNumbers = musicValues(first("catalognumber", "catalog_number", "catalogue_number"))
	m.Media = musicValues(first("media", "mediatype", "media_type"))
	m.Moods = musicValues(first("mood"))
	m.Grouping = first("grouping", "contentgroup")
	m.Comment = first("comment", "description")
	roles := []string{"conductor", "lyricist", "arranger", "producer", "director", "engineer", "mixer", "remixer", "djmixer"}
	for _, role := range roles {
		if names := musicValues(tags[role]); len(names) > 0 {
			m.Credits = append(m.Credits, model.MusicCredit{Role: role, Names: names})
		}
	}
	custom := map[string][]string{}
	var keys []string
	for key := range tags {
		if strings.HasPrefix(key, "credit:") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		role := strings.TrimSpace(strings.TrimPrefix(key, "credit:"))
		if role != "" {
			custom[role] = append(custom[role], musicValues(tags[key])...)
		}
	}
	var ordered []string
	for role := range custom {
		ordered = append(ordered, role)
	}
	sort.Strings(ordered)
	for _, role := range ordered {
		if names := musicValues(strings.Join(custom[role], "\x00")); len(names) > 0 {
			m.Credits = append(m.Credits, model.MusicCredit{Role: role, Names: names})
		}
	}
}

// structuredReleaseTag limits the comment supplement to scoped multi-values.
func structuredReleaseTag(key string) bool {
	if strings.HasPrefix(key, "credit:") {
		return true
	}
	switch key {
	case "releasetype", "release_type", "musicbrainz_albumtype", "musicbrainz album type",
		"label", "publisher", "organization", "catalognumber", "catalog_number", "catalogue_number",
		"media", "mediatype", "media_type", "mood", "conductor", "lyricist", "arranger", "producer",
		"director", "engineer", "mixer", "remixer", "djmixer":
		return true
	}
	return false
}
