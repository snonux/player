package scanner

import (
	"path"
	"strings"

	"github.com/snonux/player/internal/model"
)

// MatchKind ranks how confidently a missing audio row matches a new one.
type MatchKind int

const (
	MatchNone MatchKind = iota
	MatchStem
	MatchStrong
	MatchExact
)

// TrackIdentity is the internal fingerprint used only for move/rename
// reconciliation. API song identity remains media.id.
type TrackIdentity struct {
	RecordingMBID string
	TagKey        string
	Size          int64
	DurationMs    int64
	StemPath      string
	BaseStem      string
}

// IdentityOf builds a fingerprint from an indexed or probed audio row.
func IdentityOf(m model.Media) TrackIdentity {
	id := TrackIdentity{
		Size:       m.FileSizeBytes,
		DurationMs: int64(m.Duration*1000 + 0.5),
		StemPath:   stemPath(m.RelPath),
		BaseStem:   baseStem(m.FileName),
	}
	if m.Music != nil {
		id.RecordingMBID = normalizeMBID(m.Music.RecordingMBID)
		id.TagKey = tagFingerprint(m.Music)
	}
	return id
}

// ClassifyMatch returns the strongest applicable match signal between a
// soft-deleted row and a newly inserted row. MatchNone means do not pair.
func ClassifyMatch(missing, neu TrackIdentity) MatchKind {
	if missing.RecordingMBID != "" && missing.RecordingMBID == neu.RecordingMBID {
		return MatchExact
	}
	if missing.Size > 0 && missing.Size == neu.Size &&
		durationClose(missing.DurationMs, neu.DurationMs) &&
		missing.BaseStem != "" && missing.BaseStem == neu.BaseStem {
		return MatchExact
	}
	if missing.TagKey != "" && missing.TagKey == neu.TagKey &&
		missing.Size > 0 && missing.Size == neu.Size {
		return MatchStrong
	}
	if missing.StemPath != "" && missing.StemPath == neu.StemPath &&
		(missing.Size == neu.Size || durationClose(missing.DurationMs, neu.DurationMs)) {
		return MatchStem
	}
	return MatchNone
}

// SameTagFingerprint reports whether both identities share a non-empty tag key
// (used for the deliberate 1∶1 fallback).
func SameTagFingerprint(a, b TrackIdentity) bool {
	return a.TagKey != "" && a.TagKey == b.TagKey
}

func tagFingerprint(m *model.MusicMetadata) string {
	if m == nil {
		return ""
	}
	title := normalizedIdentity(m.Title)
	if title == "" {
		return ""
	}
	artist := normalizedIdentity(m.AlbumArtist)
	if artist == "" {
		artist = normalizedIdentity(m.Artist)
	}
	album := normalizedIdentity(m.Album)
	return strings.Join([]string{artist, album, itoa(m.Disc), itoa(m.Track), title}, "\x00")
}

func normalizedIdentity(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func normalizeMBID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 36 {
		return ""
	}
	for i, r := range value {
		switch {
		case r == '-' && (i == 8 || i == 13 || i == 18 || i == 23):
		case (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f'):
		default:
			return ""
		}
	}
	return value
}

func durationClose(a, b int64) bool {
	if a <= 0 || b <= 0 {
		return false
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= 500
}

func stemPath(rel string) string {
	rel = path.Clean("/" + strings.TrimPrefix(filepathToSlash(rel), "/"))
	ext := path.Ext(rel)
	if ext == "" {
		return strings.TrimPrefix(rel, "/")
	}
	return strings.TrimPrefix(strings.TrimSuffix(rel, ext), "/")
}

func baseStem(name string) string {
	name = path.Base(filepathToSlash(name))
	ext := path.Ext(name)
	if ext == "" {
		return strings.ToLower(name)
	}
	return strings.ToLower(strings.TrimSuffix(name, ext))
}

func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

func itoa(n int) string {
	if n <= 0 {
		return ""
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
