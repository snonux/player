package service

import (
	"reflect"
	"testing"

	"codeberg.org/snonux/player/internal/model"
)

func TestMusicArtistTopSongs(t *testing.T) {
	key := catalogKey("ar", "artist")
	song := func(id int64, title string, personal, global int) model.Media {
		return model.Media{ID: id, FileName: title, PlayCount: global,
			ListeningStats: model.ListeningStats{PersonalPlayCount: personal},
			Music:          &model.MusicMetadata{Title: title, Artist: "Artist", Composers: []string{"Artist"}}}
	}
	for _, tc := range []struct {
		name, source string
		songs        []model.Media
		ids          []int64
	}{
		{"personal wins over global", "personal", []model.Media{song(1, "A", 1, 100), song(2, "B", 2, 0), song(3, "C", 0, 500)}, []int64{2, 1}},
		{"global fallback stable ties", "global", []model.Media{song(4, "B", 0, 3), song(2, "A", 0, 3), song(1, "A", 0, 3), song(5, "Zero", 0, 0)}, []int64{1, 2, 4}},
		{"unplayed empty", "none", []model.Media{song(1, "A", 0, 0)}, []int64{}},
		{"no visible songs", "none", nil, []int64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Duplicate album membership and multiple roles cannot duplicate songs.
			albums := []Album{{Songs: tc.songs}, {Songs: tc.songs}}
			result := MusicArtistTopSongs(albums, key)
			ids := []int64{}
			for _, track := range result.Songs {
				ids = append(ids, track.ID)
			}
			if result.Source != tc.source || !reflect.DeepEqual(ids, tc.ids) {
				t.Fatalf("got %s %v; want %s %v", result.Source, ids, tc.source, tc.ids)
			}
			if got := MusicArtistTopSongs(albums, "another"); len(got.Songs) != 0 || got.Source != "none" {
				t.Fatalf("unrelated songs leaked: %+v", got)
			}
		})
	}
	songs := []model.Media{}
	for id := int64(1); id <= 12; id++ {
		songs = append(songs, song(id, "same", int(id), 0))
	}
	result := MusicArtistTopSongs([]Album{{Songs: songs}}, key)
	if len(result.Songs) != 10 || result.Songs[0].ID != 12 || result.Songs[9].ID != 3 {
		t.Fatalf("top ten bound: %+v", result)
	}
}
