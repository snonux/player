package service

import (
	"codeberg.org/snonux/player/internal/model"
	"context"
	"reflect"
	"testing"
)

func TestSearchMusic(t *testing.T) {
	source := albumSource{sets: []model.Set{{ID: 1}}, songs: []model.Media{
		{ID: 2, SetID: 1, Type: model.MediaTypeAudio, FileName: "unrelated.mp3", Music: &model.MusicMetadata{Title: "ПАРТИЯ 100%_東京", Album: "Café", Artist: "A & B; C/D", Artists: []string{"A & B; C/D", "Ünïcødé"}, AlbumArtist: "Ensemble", Track: 2, Composers: []string{"Composer Only"}}},
		{ID: 1, SetID: 1, Type: model.MediaTypeAudio, FileName: "fallback.mp3", Music: &model.MusicMetadata{Album: "Café", Artist: "A & B; C/D", AlbumArtist: "Ensemble", Track: 1}},
		{ID: 3, SetID: 2, Type: model.MediaTypeAudio, FileName: "private.mp3", Music: &model.MusicMetadata{Title: "Hidden", Album: "Secret", Artist: "Secret"}},
	}}
	albums, err := MusicAlbums(context.Background(), source, 1)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, q, artist, album string
		songs                  []int64
		albums, artists        int
	}{
		{"empty", "", "", "", nil, 0, 0},
		{"title Unicode case", "партия", "", "", []int64{2}, 0, 0},
		{"structured artist Unicode case", "ÜNÏCØDÉ", "", "", []int64{2}, 1, 1},
		{"album", "café", "", "", []int64{1, 2}, 1, 0},
		{"no accent folding", "cafe", "", "", nil, 0, 0},
		{"literal wildcard", "100%_", "", "", []int64{2}, 0, 0},
		{"real name punctuation", "A & B; C/D", "", "", []int64{1, 2}, 1, 1},
		{"album artist", "ensemble", "", "", []int64{1, 2}, 1, 1},
		{"AND constraints", "партия", "ünïcødé", "café", []int64{2}, 0, 0},
		{"wrong constraint", "партия", "missing", "café", nil, 0, 0},
		{"artist constrained album", "", "ünïcødé", "café", []int64{2}, 1, 1},
		{"missing title fallback", "fallback", "", "", []int64{1}, 0, 0},
		{"no filename over tagged title", "unrelated", "", "", nil, 0, 0},
		{"composer remains secondary", "Composer Only", "", "", nil, 0, 0},
		{"ACL hidden title", "hidden", "", "", nil, 0, 0},
		{"ACL hidden entity", "secret", "", "", nil, 0, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := SearchMusic(albums, test.q, test.artist, test.album)
			var ids []int64
			for _, song := range result.Songs {
				ids = append(ids, song.ID)
			}
			if !reflect.DeepEqual(ids, test.songs) || len(result.Albums) != test.albums || len(result.Artists) != test.artists {
				t.Fatalf("result %+v IDs%v", result, ids)
			}
			for _, album := range result.Albums {
				if album.Songs != nil || album.Discs != nil || album.SongCredits != nil {
					t.Fatal("detail embedded in summary")
				}
			}
			for _, artist := range result.Artists {
				if artist.Albums != nil {
					t.Fatal("discography embedded in summary")
				}
			}
		})
	}
	if len(albums[0].Songs) != 2 {
		t.Fatal("search mutated catalog")
	}
}
