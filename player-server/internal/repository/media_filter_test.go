package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

// TestSQLite_ListMediaCombinedFilters runs combined filters against real
// SQLite. The favorites JOIN placeholder precedes the WHERE placeholders, so
// wrongly ordered bind arguments return no rows or another user's favorites.
func TestSQLite_ListMediaCombinedFilters(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	defer s.Close()
	now := time.Now().Truncate(time.Second)
	alice := mustID(t)(s.CreateUser(ctx, &model.User{Username: "alice", PasswordHash: "h", CreatedAt: now}))
	bob := mustID(t)(s.CreateUser(ctx, &model.User{Username: "bob", PasswordHash: "h", CreatedAt: now}))
	books := mustID(t)(s.CreateSet(ctx, &model.Set{Name: "books", RootPath: "/books", CreatedAt: now}))
	music := mustID(t)(s.CreateSet(ctx, &model.Set{Name: "music", RootPath: "/music", CreatedAt: now}))
	media := func(set int64, name string) int64 {
		return mustID(t)(s.CreateMedia(ctx, &model.Media{SetID: set, RelPath: name, FileName: name, AbsPath: "/x/" + name, Type: model.MediaTypeAudio, CreatedAt: now}))
	}
	chapter := media(books, "10-chapter.mp3")
	otherChapter := media(books, "11-chapter.mp3")
	song := media(music, "10-chapter-song.mp3")
	for _, fav := range []struct{ user, media int64 }{{alice, chapter}, {alice, song}, {bob, otherChapter}} {
		if _, err := s.ToggleFavorite(ctx, fav.user, fav.media); err != nil {
			t.Fatal(err)
		}
	}
	tagID := mustID(t)(s.CreateTag(ctx, "keep"))
	for _, id := range []int64{chapter, otherChapter} {
		if err := s.AssignTag(ctx, id, tagID); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name   string
		filter MediaFilter
		want   []int64
	}{
		// File-name order: "10-chapter-song" sorts before "10-chapter." ('-' < '.').
		{"search and favorites", MediaFilter{Search: "chapter", Favorites: true, UserID: alice}, []int64{song, chapter}},
		{"search, favorites and set", MediaFilter{Search: "10-chapter", Favorites: true, UserID: alice, SetID: &books}, []int64{chapter}},
		{"search, favorites, tag and allowed sets", MediaFilter{Search: "chapter", Favorites: true, UserID: alice, Tags: []string{"keep"}, AllowedSetIDs: []int64{books}}, []int64{chapter}},
		{"other user's favorites stay separate", MediaFilter{Search: "chapter", Favorites: true, UserID: bob}, []int64{otherChapter}},
		{"no match for search outside favorites", MediaFilter{Search: "11-chapter", Favorites: true, UserID: alice}, nil},
		{"no match outside the requested set", MediaFilter{Search: "song", Favorites: true, UserID: alice, SetIDs: []int64{books}}, nil},
		{"paged combined filter", MediaFilter{Search: "chapter", Favorites: true, UserID: alice, Limit: 1, Offset: 1}, []int64{chapter}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ListMedia(ctx, tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d rows %+v, want ids %v", len(got), got, tc.want)
			}
			for i, m := range got {
				if m.ID != tc.want[i] {
					t.Fatalf("row %d id = %d, want %d", i, m.ID, tc.want[i])
				}
			}
		})
	}
}

// mustID adapts an (id, error) constructor result for concise fixture setup.
func mustID(t *testing.T) func(int64, error) int64 {
	return func(id int64, err error) int64 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
}

// TestSQLite_ListMediaFileSizeFilters checks inclusive byte bounds; the API
// parsed filesize_min/filesize_max but the query used to ignore them.
func TestSQLite_ListMediaFileSizeFilters(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	defer s.Close()
	now := time.Now().Truncate(time.Second)
	set := mustID(t)(s.CreateSet(ctx, &model.Set{Name: "s", RootPath: "/s", CreatedAt: now}))
	ids := map[int64]int64{}
	for _, size := range []int64{100, 200, 300} {
		name := fmt.Sprintf("%d.mp3", size)
		ids[size] = mustID(t)(s.CreateMedia(ctx, &model.Media{SetID: set, RelPath: name, FileName: name, AbsPath: "/s/" + name, Type: model.MediaTypeAudio, FileSizeBytes: size, CreatedAt: now}))
	}
	ptr := func(v int64) *int64 { return &v }
	cases := []struct {
		name     string
		min, max *int64
		want     []int64
	}{
		{"inclusive range", ptr(200), ptr(300), []int64{ids[200], ids[300]}},
		{"minimum only", ptr(201), nil, []int64{ids[300]}},
		{"maximum only", nil, ptr(100), []int64{ids[100]}},
		{"empty range", ptr(301), nil, nil},
		{"inverted range matches nothing", ptr(300), ptr(100), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ListMedia(ctx, MediaFilter{MinFileSize: tc.min, MaxFileSize: tc.max})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d rows, want %v", len(got), tc.want)
			}
			for i, m := range got {
				if m.ID != tc.want[i] {
					t.Fatalf("row %d id = %d, want %d", i, m.ID, tc.want[i])
				}
			}
		})
	}
}

func TestSQLite_MediaSortDirectionsAndUnknowns(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	defer s.Close()
	now := time.Now().UTC().Truncate(time.Second)
	set := mustID(t)(s.CreateSet(ctx, &model.Set{Name: "sort", RootPath: "/sort", CreatedAt: now}))
	for i, name := range []string{"b.mp4", "a.mp4", "unknown.mp4"} {
		size, duration, taken := int64((i+1)*100), float64((i+1)*10), fmt.Sprintf("2024:01:0%d 12:00:00", i+1)
		if i == 2 {
			size = 0
			duration = 0
			taken = ""
		}
		mustID(t)(s.CreateMedia(ctx, &model.Media{SetID: set, FileName: name, RelPath: name, AbsPath: "/sort/" + name, Type: model.MediaTypeVideo, CreatedAt: now.Add(time.Duration(i) * time.Hour), FileSizeBytes: size, Duration: duration, EXIFDate: taken, PlayCount: i}))
	}
	for _, tc := range []struct {
		sort string
		want []string
	}{
		{"name_asc", []string{"a.mp4", "b.mp4", "unknown.mp4"}},
		{"name_desc", []string{"unknown.mp4", "b.mp4", "a.mp4"}},
		{"size_asc", []string{"b.mp4", "a.mp4", "unknown.mp4"}},
		{"size_desc", []string{"a.mp4", "b.mp4", "unknown.mp4"}},
		{"duration_desc", []string{"a.mp4", "b.mp4", "unknown.mp4"}},
		{"date_taken_desc", []string{"a.mp4", "b.mp4", "unknown.mp4"}},
		{"date_desc", []string{"unknown.mp4", "a.mp4", "b.mp4"}},
		{"play_count_desc", []string{"unknown.mp4", "a.mp4", "b.mp4"}},
		{"last_played_desc", []string{"a.mp4", "b.mp4", "unknown.mp4"}},
	} {
		t.Run(tc.sort, func(t *testing.T) {
			got, err := s.ListMedia(ctx, MediaFilter{SetID: &set, Sort: tc.sort, UserID: 99})
			if err != nil {
				t.Fatal(err)
			}
			for i, item := range got {
				if item.FileName != tc.want[i] {
					t.Fatalf("item %d = %s; want %s", i, item.FileName, tc.want[i])
				}
			}
		})
	}
}

func TestSQLite_LastPlayedSortIsPersonal(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	defer s.Close()
	now := time.Now().UTC().Truncate(time.Second)
	alice := mustID(t)(s.CreateUser(ctx, &model.User{Username: "alice-sort", PasswordHash: "h", CreatedAt: now}))
	bob := mustID(t)(s.CreateUser(ctx, &model.User{Username: "bob-sort", PasswordHash: "h", CreatedAt: now}))
	set := mustID(t)(s.CreateSet(ctx, &model.Set{Name: "personal-sort", RootPath: "/personal", CreatedAt: now}))
	ids := []int64{}
	for _, name := range []string{"a.mp3", "b.mp3", "c.mp3"} {
		ids = append(ids, mustID(t)(s.CreateMedia(ctx, &model.Media{SetID: set, FileName: name, RelPath: name, AbsPath: "/personal/" + name, Type: model.MediaTypeAudio, CreatedAt: now})))
	}
	for _, play := range []struct {
		user, media int64
		at          time.Time
	}{{alice, ids[0], now}, {alice, ids[1], now.Add(time.Hour)}, {bob, ids[2], now.Add(2 * time.Hour)}} {
		if err := mergeMusicPlay(ctx, s.db, play.user, play.media, model.SyncedMusicPlay{ID: fmt.Sprintf("%d-%d", play.user, play.media), ObservedMS: play.at.UnixMilli()}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		sort string
		want []int64
	}{{"last_played_asc", []int64{ids[0], ids[1], ids[2]}}, {"last_played_desc", []int64{ids[1], ids[0], ids[2]}}} {
		got, err := s.ListMedia(ctx, MediaFilter{SetID: &set, UserID: alice, Sort: tc.sort})
		if err != nil {
			t.Fatal(err)
		}
		for i, item := range got {
			if item.ID != tc.want[i] {
				t.Fatalf("%s item %d = %d; want %d", tc.sort, i, item.ID, tc.want[i])
			}
		}
	}
}
