package service

import (
	"github.com/snonux/player/internal/model"
	"reflect"
	"testing"
)

func TestMusicMixRankingRatingsAndSeeds(t *testing.T) {
	song := func(id int64, artist string, genres []string, rating int) model.Media {
		return model.Media{ID: id, Rating: rating, Music: &model.MusicMetadata{Title: artist, Artist: artist, Genres: genres}}
	}
	albums := []Album{{Key: "seed", Title: "Seed", Songs: []model.Media{
		song(1, "One", []string{"Jazz"}, 3),
		song(4, "One", []string{"jazz"}, 5),
	}}, {Key: "other", Title: "Other", Songs: []model.Media{
		song(2, "Two", []string{"Jazz"}, 4),
		song(3, "Three", []string{"Rock"}, 5),
		song(4, "One", []string{"Jazz"}, 5), // canonical song occurs once
		song(5, "One", nil, 0),
	}}, {Key: "untagged", Songs: []model.Media{song(6, "Unknown artist", nil, 0)}}}
	ids := func(m MusicMix) []int64 {
		result := []int64{}
		for _, s := range m.Songs {
			result = append(result, s.ID)
		}
		return result
	}
	for _, tc := range []struct {
		kind, key string
		min       int
		want      []int64
		found     bool
	}{
		{"song", "1", 0, []int64{1, 4, 2, 5}, true},
		{"song", "1", 4, []int64{4, 2}, true},
		{"song", "1", 5, []int64{4}, true},
		{"album", "seed", 0, []int64{1, 4, 2, 5}, true},
		{"song", "6", 0, []int64{}, true},
		{"song", "missing", 0, []int64{}, false},
	} {
		got, found := BuildMusicMix(albums, tc.kind, tc.key, tc.min)
		if found != tc.found || !reflect.DeepEqual(ids(got), tc.want) {
			t.Fatalf("%+v got=%v found=%v", tc, ids(got), found)
		}
		again, _ := BuildMusicMix(albums, tc.kind, tc.key, tc.min)
		if !reflect.DeepEqual(got, again) {
			t.Fatal("non-deterministic result")
		}
	}
	for _, artist := range MusicArtists(albums) {
		if artist.Name != "One" {
			continue
		}
		got, found := BuildMusicMix(albums, "artist", artist.Key, 0)
		if !found || !reflect.DeepEqual(ids(got), []int64{1, 4, 2, 5}) {
			t.Fatalf("artist got %v found %v", ids(got), found)
		}
	}
}

func TestMusicMixFiltersBeforeLimit(t *testing.T) {
	songs := []model.Media{}
	for id := int64(1); id <= 70; id++ {
		rating := 0
		if id > 10 {
			rating = 5
		}
		songs = append(songs, model.Media{ID: id, Rating: rating, Music: &model.MusicMetadata{Artist: "One", Genres: []string{"Jazz"}}})
	}
	got, _ := BuildMusicMix([]Album{{Key: "seed", Songs: songs}}, "song", "1", 5)
	if len(got.Songs) != 50 || got.Songs[0].ID != 11 || got.Songs[49].ID != 60 {
		t.Fatalf("filter/limit %+v", got.Songs)
	}
}
