package scanner

import (
	"testing"

	"github.com/snonux/player/internal/model"
)

func TestClassifyMatch_RecordingMBID(t *testing.T) {
	mbid := "12345678-1234-1234-abcd-123456789012"
	a := IdentityOf(model.Media{Music: &model.MusicMetadata{RecordingMBID: mbid}, FileSizeBytes: 1})
	b := IdentityOf(model.Media{Music: &model.MusicMetadata{RecordingMBID: mbid}, FileSizeBytes: 99})
	if ClassifyMatch(a, b) != MatchExact {
		t.Fatalf("want exact MBID match")
	}
}

func TestClassifyMatch_SizeDurationBasename(t *testing.T) {
	a := IdentityOf(model.Media{FileName: "01-song.flac", RelPath: "A/01-song.flac", FileSizeBytes: 1000, Duration: 12.0})
	b := IdentityOf(model.Media{FileName: "01-song.mp3", RelPath: "B/01-song.mp3", FileSizeBytes: 1000, Duration: 12.2})
	if ClassifyMatch(a, b) != MatchExact {
		t.Fatalf("want exact size/duration/stem match, got %v", ClassifyMatch(a, b))
	}
}

func TestClassifyMatch_TagAndSize(t *testing.T) {
	music := &model.MusicMetadata{Title: "Neon", AlbumArtist: "Mira", Album: "Night", Disc: 1, Track: 2}
	a := IdentityOf(model.Media{Music: music, FileSizeBytes: 50, RelPath: "old/a.flac", FileName: "a.flac"})
	b := IdentityOf(model.Media{Music: music, FileSizeBytes: 50, RelPath: "new/b.flac", FileName: "b.flac"})
	if ClassifyMatch(a, b) != MatchStrong {
		t.Fatalf("want strong tag+size match")
	}
}

func TestPairIdentities_AmbiguousConflict(t *testing.T) {
	music := &model.MusicMetadata{Title: "Same", AlbumArtist: "X", Album: "Y", Track: 1}
	missing := []model.Media{{ID: 1, Type: model.MediaTypeAudio, RelPath: "old.flac", FileName: "old.flac", FileSizeBytes: 10, Music: music}}
	created := []model.Media{
		{ID: 2, Type: model.MediaTypeAudio, RelPath: "a.flac", FileName: "a.flac", FileSizeBytes: 10, Music: music},
		{ID: 3, Type: model.MediaTypeAudio, RelPath: "b.flac", FileName: "b.flac", FileSizeBytes: 10, Music: music},
	}
	pairs := PairIdentities(missing, created)
	if len(pairs) != 1 || !pairs[0].Ambiguous || len(pairs[0].Candidates) != 2 {
		t.Fatalf("want one ambiguous conflict with 2 candidates: %+v", pairs)
	}
}

func TestPairIdentities_OneToOneTagFallback(t *testing.T) {
	music := &model.MusicMetadata{Title: "Solo", AlbumArtist: "A", Album: "B", Track: 1}
	missing := []model.Media{{ID: 1, RelPath: "gone.flac", FileName: "gone.flac", FileSizeBytes: 1, Music: music}}
	created := []model.Media{{ID: 2, RelPath: "here.flac", FileName: "here.flac", FileSizeBytes: 999, Music: music}}
	pairs := PairIdentities(missing, created)
	if len(pairs) != 1 || pairs[0].Ambiguous || pairs[0].New.ID != 2 {
		t.Fatalf("want 1:1 tag fingerprint merge: %+v", pairs)
	}
}

func TestPairIdentities_ClearRename(t *testing.T) {
	missing := []model.Media{{
		ID: 7, RelPath: "Artist/Album/01-track.flac", FileName: "01-track.flac",
		FileSizeBytes: 2048, Duration: 180, Music: &model.MusicMetadata{Title: "Track"},
	}}
	created := []model.Media{{
		ID: 9, RelPath: "Artist/Album/renamed.flac", FileName: "01-track.flac",
		FileSizeBytes: 2048, Duration: 180.1, Music: &model.MusicMetadata{Title: "Track"},
	}}
	pairs := PairIdentities(missing, created)
	if len(pairs) != 1 || pairs[0].Ambiguous || pairs[0].Missing.ID != 7 || pairs[0].New.ID != 9 {
		t.Fatalf("want clear rename pair: %+v", pairs)
	}
}
