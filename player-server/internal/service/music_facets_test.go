package service

import (
	"github.com/snonux/player/internal/model"
	"net/url"
	"testing"
)

func TestMusicFacetsSameSong(t *testing.T) {
	albums := []Album{{Key: "visible", Songs: []model.Media{
		{ID: 1, Music: &model.MusicMetadata{Genres: []string{"Ambient"}, Year: 2021, Labels: []string{"Label A; B"}}},
		{ID: 2, Music: &model.MusicMetadata{Genres: []string{"Jazz"}, Year: 2019, Composers: []string{"Composer"}, Compilation: true}},
	}}}
	cases := []struct {
		name  string
		query url.Values
		count int
	}{
		{"same song", url.Values{"genre": {" ambient "}, "year": {"2020-2029"}}, 1},
		{"no cross song", url.Values{"genre": {"Ambient"}, "role": {"composer"}}, 0},
		{"or values", url.Values{"genre": {"Ambient", "Jazz"}}, 2},
		{"punctuation literal", url.Values{"label": {"Label A"}}, 0},
		{"punctuation exact", url.Values{"label": {"Label A; B"}}, 1},
		{"not marked", url.Values{"compilation": {"false"}}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filter, err := ParseMusicFilter(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			got := FilterMusic(albums, filter)
			count := 0
			for _, album := range got {
				count += album.SongCount
			}
			if count != tc.count {
				t.Fatalf("got%d want%d", count, tc.count)
			}
		})
	}
	if len(albums[0].Songs) != 2 {
		t.Fatal("mutated input")
	}
	options := MusicFacetOptions(albums)
	if len(options["genre"]) != 2 {
		t.Fatal(options)
	}
}
func TestMusicFacetValidation(t *testing.T) {
	for key, value := range map[string]string{"year": "2029-2020", "role": "producer", "compilation": "yes"} {
		if _, err := ParseMusicFilter(url.Values{key: {value}}); err == nil {
			t.Fatalf("accepted%s", key)
		}
	}
}
