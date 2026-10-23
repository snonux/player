package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

func TestDiscoverAlbums(t *testing.T) {
	old, recent := time.Unix(100, 0), time.Unix(200, 0)
	albums := []Album{
		{Key: "a", Title: "A", AddedAt: old, Rating: 2, ListeningStats: model.ListeningStats{PersonalPlayCount: 9, LastPlayedAt: &old}},
		{Key: "b", Title: "B", AddedAt: recent, IsFavorite: true, Rating: 5, ListeningStats: model.ListeningStats{PersonalPlayCount: 2, LastPlayedAt: &recent}},
		{Key: "c", Title: "C", AddedAt: old},
	}
	for _, tc := range []struct {
		name string
		want []string
	}{
		{"recently_added", []string{"b", "a", "c"}}, {"recently_played", []string{"b", "a"}}, {"most_played", []string{"a", "b"}}, {"favorites", []string{"b"}}, {"top_rated", []string{"b", "a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DiscoverAlbums(albums, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			keys := []string{}
			for _, a := range got {
				keys = append(keys, a.Key)
			}
			if !reflect.DeepEqual(keys, tc.want) {
				t.Fatalf("keys %v want %v", keys, tc.want)
			}
		})
	}
	if _, err := DiscoverAlbums(albums, "unknown"); err == nil {
		t.Fatal("accepted unknown collection")
	}
	random, err := DiscoverAlbums(albums, "random")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, a := range random {
		seen[a.Key] = true
	}
	if len(random) != 3 || len(seen) != 3 {
		t.Fatalf("random lost/duplicated albums: %v", random)
	}
	if albums[0].Key != "a" {
		t.Fatal("mutated catalog order")
	}
	for _, name := range []string{"recently_played", "most_played", "favorites", "top_rated"} {
		got, err := DiscoverAlbums([]Album{}, name)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("%s empty: %v %v", name, got, err)
		}
	}
}

func TestMusicAlbumAddedDateUsesLatestVisibleSong(t *testing.T) {
	older, latest, hidden := time.Unix(100, 0), time.Unix(200, 0), time.Unix(300, 0)
	songs := []model.Media{}
	for i, date := range []time.Time{older, latest, hidden} {
		set := int64(1)
		if i == 2 {
			set = 2
		}
		songs = append(songs, model.Media{ID: int64(i + 1), SetID: set, Type: model.MediaTypeAudio, FileName: "song.mp3", CreatedAt: date, Music: &model.MusicMetadata{Album: "Album", Artist: "Artist"}})
	}
	albums, err := MusicAlbums(context.Background(), albumSource{sets: []model.Set{{ID: 1}}, songs: songs}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 1 || !albums[0].AddedAt.Equal(latest) {
		t.Fatalf("latest authorized addition: %+v", albums)
	}
}
