package service

import (
	"github.com/snonux/player/internal/model"
	"testing"
)

func TestMusicArtistsRolesAndCounts(t *testing.T) {
	song := model.Media{ID: 1, Music: &model.MusicMetadata{Artist: "A & B; C/D", Artists: []string{"A & B; C/D", "東京"}, AlbumArtists: []string{"Compilation", "東京"}, Composers: []string{"Composer", " composer "}, Performers: []string{"Player"}}}
	second := song
	second.ID = 2
	people := MusicArtists([]Album{{Key: "al_one", Songs: []model.Media{song, second}}, {Key: "al_two", Songs: []model.Media{song}}})
	if len(people) != 5 {
		t.Fatalf("punctuation or normalized names split: %+v", people)
	}
	for _, person := range people {
		if person.AlbumCount != 2 || person.SongCount != 2 {
			t.Fatalf("duplicate counts %+v", person)
		}
		switch person.Name {
		case "A & B; C/D":
			if !containsRole(person.Roles, "track_artist") || containsRole(person.Roles, "album_artist") {
				t.Fatalf("track roles %+v", person)
			}
		case "東京":
			if !containsRole(person.Roles, "track_artist") || !containsRole(person.Roles, "album_artist") {
				t.Fatalf("combined roles %+v", person)
			}
		case "Composer":
			if len(person.Roles) != 1 || person.Roles[0] != "composer" {
				t.Fatalf("composer %+v", person)
			}
		}
		for _, album := range person.Albums {
			if album.Songs != nil || album.SongCredits != nil {
				t.Fatal("discography embeds song payloads")
			}
		}
	}
	unknown := MusicCredits(model.Media{})
	if len(unknown) != 1 || unknown[0].Name != "Unknown artist" || len(unknown[0].Roles) != 2 {
		t.Fatalf("missing credit fallback %+v", unknown)
	}
}
