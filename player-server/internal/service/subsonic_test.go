package service

import (
	"context"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

type subsonicCatalog struct {
	sets  []model.Set
	songs []model.Media
}

func (s subsonicCatalog) ListSets(context.Context, int64) ([]model.Set, error) {
	return s.sets, nil
}

func (s subsonicCatalog) ListMedia(context.Context, int64, MediaQueryFilter) ([]model.Media, error) {
	return s.songs, nil
}

func TestSubsonicServiceBrowseAndSearch(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	song := model.Media{
		ID: 11, SetID: 1, Type: model.MediaTypeAudio, FileName: "track.flac", RelPath: "Kei/Album/track.flac",
		Duration: 120, Bitrate: 320000, FileSizeBytes: 1000, CreatedAt: now,
		Music: &model.MusicMetadata{Title: "Harbour", Artist: "Kei", Album: "Shores", AlbumArtist: "Kei", Track: 1, Year: 2024},
	}
	denied := model.Media{
		ID: 99, SetID: 2, Type: model.MediaTypeAudio, FileName: "secret.mp3", RelPath: "secret.mp3", CreatedAt: now,
		Music: &model.MusicMetadata{Title: "Secret", Artist: "Hidden", Album: "Vault"},
	}
	source := subsonicCatalog{
		sets:  []model.Set{{ID: 1, Name: "Music"}, {ID: 2, Name: "Restricted"}, {ID: 3, Name: "Shows", IsPodcast: true}},
		songs: []model.Media{song, denied},
	}
	// CatalogSource ListSets already ACL-filters; simulate permitted set 1 only.
	source.sets = []model.Set{{ID: 1, Name: "Music"}}
	source.songs = []model.Media{song}

	svc := NewSubsonicService(source)
	folders, err := svc.MusicFolders(context.Background(), 1)
	if err != nil || len(folders) != 1 || folders[0].Name != "Music" {
		t.Fatalf("folders: %v %v", folders, err)
	}
	index, err := svc.ArtistIndex(context.Background(), 1, "")
	if err != nil || len(index) == 0 {
		t.Fatalf("index: %v %v", index, err)
	}
	var artistID string
	for _, letter := range index {
		for _, artist := range letter.Artist {
			if artist.Name == "Kei" {
				artistID = artist.ID
			}
		}
	}
	if artistID == "" {
		t.Fatal("missing Kei")
	}
	artist, err := svc.ArtistDetail(context.Background(), 1, artistID, "")
	if err != nil || artist == nil || len(artist.Album) != 1 {
		t.Fatalf("artist: %+v %v", artist, err)
	}
	albumID := artist.Album[0].ID
	album, err := svc.AlbumDetail(context.Background(), 1, albumID, "")
	if err != nil || album == nil || len(album.Song) != 1 || album.Song[0].Title != "Harbour" {
		t.Fatalf("album: %+v %v", album, err)
	}
	gotSong, err := svc.SongDetail(context.Background(), 1, "11", "")
	if err != nil || gotSong == nil || gotSong.Title != "Harbour" {
		t.Fatalf("song: %+v %v", gotSong, err)
	}
	search, err := svc.Search(context.Background(), 1, "Harbour", "", 20, 0, 20, 0, 20, 0)
	if err != nil || len(search.Song) != 1 {
		t.Fatalf("search: %+v %v", search, err)
	}
	// Denied library via musicFolderId that has no songs for this user.
	empty, err := svc.Search(context.Background(), 1, "Harbour", "2", 20, 0, 20, 0, 20, 0)
	if err != nil || len(empty.Song) != 0 {
		t.Fatalf("folder filter: %+v %v", empty, err)
	}
}
