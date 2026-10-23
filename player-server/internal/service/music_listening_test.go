package service

import (
	"context"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

func TestPersonalMusicListeningThresholdAccountsAndRetry(t *testing.T) {
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	clk := &clock.MockClock{T: now}
	svc := NewProgressService(store, clk)
	users := make([]int64, 2)
	for i, name := range []string{"first", "second"} {
		users[i], err = store.CreateUser(ctx, &model.User{Username: name, PasswordHash: "hash", IsAdmin: true, CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
	}
	set, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "/music", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	media, err := store.CreateMedia(ctx, &model.Media{SetID: set, Type: model.MediaTypeAudio, FileName: "song.mp3", RelPath: "song.mp3", AbsPath: "/music/song.mp3", CreatedAt: now, Music: &model.MusicMetadata{Album: "Album", Artist: "Artist"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range users {
		for i, pos := range []float64{0, 10, 20, 30, 40, 50} {
			clk.T = now.Add(time.Duration(i+1) * time.Second)
			if err := svc.UpdateProgress(ctx, "same-session", user, media, pos); err != nil {
				t.Fatal(err)
			}
		}
		stats, err := store.ListMusicListening(ctx, user)
		if err != nil || stats[media].PersonalPlayCount != 0 {
			t.Fatalf("below threshold: %+v %v", stats, err)
		}
		clk.T = now.Add(7 * time.Second)
		if err := svc.UpdateProgress(ctx, "same-session", user, media, 60); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err := svc.BatchUpdateProgress(ctx, "same-session", user, []ProgressUpdate{{MediaID: media, PositionSeconds: 60, ObservedAt: clk.T}, {MediaID: media, PositionSeconds: 70, ObservedAt: clk.T.Add(time.Second)}}); err != nil {
				t.Fatal(err)
			}
		}
		stats, err = store.ListMusicListening(ctx, user)
		if err != nil || stats[media].PersonalPlayCount != 1 || stats[media].LastPlayedAt == nil || !stats[media].LastPlayedAt.Equal(clk.T) {
			t.Fatalf("qualified %+v %v", stats, err)
		}
	}
	// A new session must meet the threshold independently; a seek earns only 12s.
	clk.T = now.Add(10 * time.Second)
	if err := svc.UpdateProgress(ctx, "new", users[0], media, 1000); err != nil {
		t.Fatal(err)
	}
	stats, err := store.ListMusicListening(ctx, users[0])
	if err != nil || stats[media].PersonalPlayCount != 1 {
		t.Fatalf("seek counted: %+v %v", stats, err)
	}
	browse := NewMediaService(store, clk, "/music", nil, nil)
	albums, err := MusicAlbums(ctx, browse, users[0])
	if err != nil || len(albums) != 1 || albums[0].PersonalPlayCount != 1 {
		t.Fatalf("albums %+v %v", albums, err)
	}
	artists := MusicArtists(albums)
	if len(artists) != 1 || artists[0].PersonalPlayCount != 1 {
		t.Fatalf("artists %+v", artists)
	}
	// Roll back personal qualification together with the rest of the transaction.
	sentinel := context.Canceled
	err = store.WithProgressTransaction(ctx, func(tx repository.ProgressUpdateStore) error {
		personal := tx.(repository.MusicProgressStore)
		for i := 0; i < 7; i++ {
			if err := personal.ApplyMusicProgress(ctx, users[0], "rollback", media, float64(i*10), 0, now.Add(time.Duration(20+i)*time.Second)); err != nil {
				return err
			}
		}
		return sentinel
	})
	if err != sentinel {
		t.Fatal(err)
	}
	stats, err = store.ListMusicListening(ctx, users[0])
	if err != nil || stats[media].PersonalPlayCount != 1 {
		t.Fatalf("rollback %+v %v", stats, err)
	}

	// A reset changes resume only. Older packets and the same session cannot
	// earn another personal play; a durable offline event can, exactly once.
	clk.T = now.Add(40 * time.Second)
	if err := svc.MarkNotStarted(ctx, users[0], media); err != nil {
		t.Fatal(err)
	}
	updates := []ProgressUpdate{}
	for i := 0; i < 8; i++ {
		updates = append(updates, ProgressUpdate{MediaID: media, PositionSeconds: float64(i * 10), ObservedAt: now.Add(time.Duration(i+1) * time.Second)})
	}
	if err := svc.BatchUpdateProgress(ctx, "same-session", users[0], updates); err != nil {
		t.Fatal(err)
	}
	event := model.PlaybackSync{MusicPlays: []model.SyncedMusicPlay{{ID: "0123456789abcdef0123456789abcdef", ObservedMS: now.Add(30 * time.Second).UnixMilli(), AccumulatedSeconds: 60}}}
	for i := 0; i < 2; i++ {
		result, err := SyncPlayback(ctx, store, users[0], media, event)
		if err != nil || len(result.AcceptedMusicPlays) != 1 {
			t.Fatalf("event %+v %v", result, err)
		}
	}
	stats, err = store.ListMusicListening(ctx, users[0])
	if err != nil || stats[media].PersonalPlayCount != 2 {
		t.Fatalf("event retry/reset %+v %v", stats, err)
	}
	if _, err := SyncPlayback(ctx, store, users[1], media, event); err != nil {
		t.Fatal(err)
	}
	stats, err = store.ListMusicListening(ctx, users[1])
	if err != nil || stats[media].PersonalPlayCount != 2 {
		t.Fatalf("event account %+v %v", stats, err)
	}
	for _, name := range []string{"book.m4b", "episode.mp3"} {
		source := set
		if name == "episode.mp3" {
			source, err = store.CreateSet(ctx, &model.Set{Name: "Podcasts", RootPath: "/pod", IsPodcast: true, CreatedAt: now})
			if err != nil {
				t.Fatal(err)
			}
		}
		id, err := store.CreateMedia(ctx, &model.Media{SetID: source, Type: model.MediaTypeAudio, FileName: name, RelPath: name, AbsPath: "/music/" + name, CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := SyncPlayback(ctx, store, users[0], id, event); err != nil {
			t.Fatal(err)
		}
		stats, err = store.ListMusicListening(ctx, users[0])
		if err != nil || stats[id].PersonalPlayCount != 0 {
			t.Fatalf("excluded %s %+v %v", name, stats, err)
		}
	}
	if err := store.DeleteUser(ctx, users[0]); err != nil {
		t.Fatal(err)
	}
	stats, err = store.ListMusicListening(ctx, users[0])
	if err != nil || len(stats) != 0 {
		t.Fatalf("cleanup %+v %v", stats, err)
	}
}

func TestPersonalMusicAggregatesUniqueRolesAndFilteredSongs(t *testing.T) {
	first, last := time.Unix(100, 0).UTC(), time.Unix(200, 0).UTC()
	songs := []model.Media{
		{ID: 1, ListeningStats: model.ListeningStats{PersonalPlayCount: 2, LastPlayedAt: &first}, Music: &model.MusicMetadata{Artist: "Person", AlbumArtist: "Person", Composers: []string{"Person"}, Genres: []string{"Jazz"}}},
		{ID: 2, ListeningStats: model.ListeningStats{PersonalPlayCount: 3, LastPlayedAt: &last}, Music: &model.MusicMetadata{Artist: "Person", Genres: []string{"Rock"}}},
	}
	album := Album{Key: "album", Songs: songs, SongCount: 2}
	for _, song := range songs {
		album.ListeningStats.Add(song.ListeningStats)
	}
	artists := MusicArtists([]Album{album})
	if len(artists) != 1 || artists[0].PersonalPlayCount != 5 || !artists[0].LastPlayedAt.Equal(last) {
		t.Fatalf("unique role totals %+v", artists)
	}
	filtered := FilterMusic([]Album{album}, MusicFilter{"genre": {"Jazz"}})
	if len(filtered) != 1 || filtered[0].PersonalPlayCount != 2 || !filtered[0].LastPlayedAt.Equal(first) {
		t.Fatalf("filtered totals %+v", filtered)
	}
}
