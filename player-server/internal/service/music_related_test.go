package service

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"codeberg.org/snonux/player/internal/model"
)

func TestMusicRelatedMetadataRankingAndEmpty(t *testing.T) {
	songs := []model.Media{}
	for i, m := range []*model.MusicMetadata{
		{Title: "Source", Album: "Source", Artist: "One", Genres: []string{"Jazz"}},
		{Title: "Both", Album: "Both", Artist: "One", Genres: []string{" jazz "}},
		{Title: "Genre", Album: "Genre", Artist: "Two", Genres: []string{"Jazz"}},
		{Title: "Unrelated", Album: "Unrelated", Artist: "Three", Genres: []string{"Rock"}},
		{},
	} {
		songs = append(songs, model.Media{ID: int64(i + 1), SetID: 1, Type: model.MediaTypeAudio, FileName: strconv.Itoa(i) + ".mp3", RelPath: strconv.Itoa(i) + "/a.mp3", Music: m})
	}
	albums, err := MusicAlbums(context.Background(), albumSource{sets: []model.Set{{ID: 1}}, songs: songs}, 1)
	if err != nil {
		t.Fatal(err)
	}
	got, found := MusicRelated(albums, "song", "1")
	if !found || len(got) != 2 || got[0].Key != "2" || got[1].Key != "3" || len(got[0].Basis) != 2 {
		t.Fatalf("ranking %+v found=%v", got, found)
	}
	again, _ := MusicRelated(albums, "song", "1")
	if !reflect.DeepEqual(got, again) {
		t.Fatal("unstable matches")
	}
	for _, album := range albums {
		if album.Title == "Source" {
			related, ok := MusicRelated(albums, "album", album.Key)
			if !ok || len(related) != 2 || related[0].Title != "Both" {
				t.Fatalf("album %+v", related)
			}
		}
	}
	for _, artist := range MusicArtists(albums) {
		related, ok := MusicRelated(albums, "artist", artist.Key)
		if !ok {
			t.Fatalf("valid artist unavailable: %s", artist.Name)
		}
		if artist.Name == "One" && (len(related) != 1 || related[0].Title != "Two") {
			t.Fatalf("artist %+v", related)
		}
		if artist.Name == "Unknown artist" && len(related) != 0 {
			t.Fatal("fallback matches")
		}
	}
	empty, ok := MusicRelated(albums, "song", "5")
	if !ok || len(empty) != 0 {
		t.Fatal("untagged source")
	}
	_, ok = MusicRelated(albums, "song", "999")
	if ok {
		t.Fatal("missing source found")
	}
}
