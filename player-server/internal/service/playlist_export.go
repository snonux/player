package service

import (
	"context"
	"fmt"
	"math"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ExportM3U returns an extended UTF-8 playlist with local media-root-relative
// paths, preserving only the requesting viewer's accessible occurrences.
func (s *PlaylistService) ExportM3U(ctx context.Context, user int64, id string) (string, error) {
	p, err := s.Get(ctx, user, id)
	if err != nil {
		return "", err
	}
	sets, err := s.source.ListSets(ctx, user)
	if err != nil {
		return "", fmt.Errorf("export playlist sets: %w", err)
	}
	roots := make(map[int64]string, len(sets))
	for _, set := range sets {
		roots[set.ID] = set.RootPath
	}
	var out strings.Builder
	_, _ = fmt.Fprintf(&out, "#EXTM3U\n#PLAYLIST:%s\n# Only accessible songs with exportable local paths are included.\n", m3uText(p.Name))
	for _, song := range p.Songs {
		root, ok := roots[song.SetID]
		if !ok || !m3uLocalPath(root) || !m3uLocalPath(song.RelPath) {
			continue
		}
		title := strings.TrimSuffix(song.FileName, path.Ext(song.FileName))
		if song.Music != nil {
			if strings.TrimSpace(song.Music.Title) != "" {
				title = song.Music.Title
			}
			if strings.TrimSpace(song.Music.Artist) != "" {
				title = song.Music.Artist + " - " + title
			}
		}
		seconds := -1.0
		if song.Duration > 0 && !math.IsNaN(song.Duration) && !math.IsInf(song.Duration, 0) {
			seconds = math.Floor(song.Duration)
		}
		_, _ = fmt.Fprintf(&out, "#EXTINF:%.0f,%s\n./%s/%s\n", seconds, m3uText(title), root, song.RelPath)
	}
	return out.String(), nil
}

func m3uText(value string) string {
	return strings.Join(strings.FieldsFunc(strings.ToValidUTF8(value, "�"), func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }), " ")
}

func m3uLocalPath(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
