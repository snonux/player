package service

import (
	"context"
	"testing"

	"codeberg.org/snonux/player/internal/model"
)

type albumSource struct {
	sets  []model.Set
	songs []model.Media
}

func (s albumSource) ListSets(context.Context, int64) ([]model.Set, error) { return s.sets, nil }
func (s albumSource) ListMedia(context.Context, int64, MediaQueryFilter) ([]model.Media, error) {
	return s.songs, nil
}
func TestMusicAlbums(t *testing.T) {
	song := func(id, set int64, disc, track int, folder, title, artist string) model.Media {
		return model.Media{ID: id, SetID: set, Type: model.MediaTypeAudio, FileName: folder + ".mp3", RelPath: folder + "/song.mp3", Duration: 12, ThumbnailPath: "cover", Music: &model.MusicMetadata{Album: title, AlbumArtist: artist, Disc: disc, Track: track}}
	}
	songs := []model.Media{song(1, 1, 2, 1, "Disc2", " Album  ", " A "), song(2, 2, 1, 2, "Disc1", "album", "a"), song(3, 1, 1, 1, "Disc1", "ALBUM", "A"), song(4, 3, 1, 1, "hidden", "album", "a"), song(5, 4, 1, 1, "podcast", "album", "a"), song(6, 5, 1, 1, "book", "album", "a"), song(7, 1, 0, 0, "unknown", "album", "a")}
	albums, err := MusicAlbums(context.Background(), albumSource{sets: []model.Set{{ID: 1}, {ID: 2}, {ID: 4, IsPodcast: true}, {ID: 5, Name: "Audiobooks"}}, songs: songs}, 1)
	if err != nil || len(albums) != 1 {
		t.Fatalf("albums=%+v err=%v", albums, err)
	}
	a := albums[0]
	if a.SongCount != 4 || a.Duration != 48 || a.CoverMediaID != 3 {
		t.Fatalf("totals/art %+v", a)
	}
	for i, id := range []int64{3, 2, 1, 7} {
		if a.Songs[i].ID != id {
			t.Fatalf("order %+v", a.Songs)
		}
	}
}
func TestAlbumIdentity(t *testing.T) {
	base := model.Media{SetID: 1, RelPath: "Release/Disc 1/x.mp3", Music: &model.MusicMetadata{Album: " X ", AlbumArtist: " A ", Year: 2000}}
	a := albumForSong(base, model.Set{})
	base.RelPath = "Release/Disc 2/y.mp3"
	base.Music.Year = 2020
	base.Music.Album = "x"
	base.Music.AlbumArtist = "a"
	if albumForSong(base, model.Set{}).Key != a.Key {
		t.Fatal("year/folder split album")
	}
	base.Music.ReleaseMBID = "INVALID"
	if albumForSong(base, model.Set{}).Key != a.Key {
		t.Fatal("invalid MBID used")
	}
	base.Music.ReleaseMBID = "12345678-1234-1234-ABCD-123456789012"
	mb := albumForSong(base, model.Set{}).Key
	base.Music.ReleaseMBID = "12345678-1234-1234-abcd-123456789012"
	base.Music.Album = "renamed"
	if albumForSong(base, model.Set{}).Key != mb || mb == a.Key {
		t.Fatal("MBID identity")
	}
	base.Music = &model.MusicMetadata{}
	a = albumForSong(base, model.Set{Name: "Set"})
	base.SetID = 2
	if !a.FromFolder || a.Key == albumForSong(base, model.Set{}).Key {
		t.Fatal("folder fallback leaked across sets")
	}
}
