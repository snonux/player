package service

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

func TestSmartPlaylistDynamicPersonalACLAndManualRecovery(t *testing.T) {
	ctx := context.Background()
	store, err := repository.Open(filepath.Join(t.TempDir(), "smart.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Now().UTC()
	id := func(value int64, err error) int64 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	owner := id(store.CreateUser(ctx, &model.User{Username: "owner", PasswordHash: "x", CreatedAt: now}))
	other := id(store.CreateUser(ctx, &model.User{Username: "other", PasswordHash: "x", CreatedAt: now}))
	set := id(store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: now}))
	hidden := id(store.CreateSet(ctx, &model.Set{Name: "Hidden", RootPath: "Hidden", CreatedAt: now}))
	for _, user := range []int64{owner, other} {
		if err := store.GrantPermission(ctx, &model.SetPermission{UserID: user, SetID: set, Role: model.RoleViewer}); err != nil {
			t.Fatal(err)
		}
	}
	songs := []int64{}
	for i, title := range []string{"B", "A", "Hidden"} {
		setID := set
		if i == 2 {
			setID = hidden
		}
		songs = append(songs, id(store.CreateMedia(ctx, &model.Media{SetID: setID, FileName: title + ".mp3", RelPath: title + ".mp3", Type: model.MediaTypeAudio, Music: &model.MusicMetadata{Title: title, Album: "Album", Artist: "Artist", Genres: []string{"Synth"}}, CreatedAt: now})))
	}
	source := NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil)
	svc := NewPlaylistService(store, source)
	shared := "shared"
	p, err := svc.CreateDetails(ctx, owner, "Smart", "", &shared)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EditSongs(ctx, owner, p.ID, "add", "", "", []int64{songs[0]}); err != nil {
		t.Fatal(err)
	}
	rules := &model.PlaylistRules{Match: "all", Conditions: []model.PlaylistCondition{{Field: "genre", Operator: "contains", Value: "synth"}, {Field: "rating", Operator: "gte", Value: "4"}, {Field: "starred", Operator: "is", Value: "true"}}, Sort: "title", Limit: 1}
	for _, song := range songs {
		if err := store.SetMusicRating(ctx, owner, "song", strconv.FormatInt(song, 10), 5); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ToggleFavorite(ctx, owner, song); err != nil {
			t.Fatal(err)
		}
	}
	p, err = svc.SetRules(ctx, owner, p.ID, rules)
	if err != nil {
		t.Fatal(err)
	}
	if p.SongCount != 1 || p.Songs[0].ID != songs[1] {
		t.Fatalf("ordered limited ACL result %+v", p)
	}
	firstEntry := p.Entries[0].ID
	p, err = svc.Get(ctx, owner, p.ID)
	if err != nil || p.Entries[0].ID != firstEntry {
		t.Fatalf("stable result %+v %v", p, err)
	}
	foreign, err := svc.Get(ctx, other, p.ID)
	if err != nil || foreign.SongCount != 0 {
		t.Fatalf("viewer data %+v %v", foreign, err)
	}
	if _, err = svc.SetRules(ctx, other, p.ID, rules); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign edit %v", err)
	}
	if _, err = svc.EditSongs(ctx, owner, p.ID, "add", "", "", []int64{songs[0]}); err == nil {
		t.Fatal("manual edit accepted")
	}
	if err = svc.AddToPlaylists(ctx, owner, []string{p.ID}, []int64{songs[0]}, "skip"); err == nil {
		t.Fatal("batch manual edit accepted")
	}
	if ok, err := store.AppendPlaylist(ctx, owner, p.ID, []model.PlaylistEntry{{ID: "late", MediaID: songs[0]}}); err != nil || ok {
		t.Fatalf("racing append %v %v", ok, err)
	}
	if _, err := store.ToggleFavorite(ctx, owner, songs[1]); err != nil {
		t.Fatal(err)
	}
	lists, err := svc.List(ctx, owner)
	if err != nil || len(lists) != 1 || lists[0].SongCount != 1 {
		t.Fatalf("list refresh %+v %v", lists, err)
	}
	p, err = svc.Get(ctx, owner, p.ID)
	if err != nil || p.Songs[0].ID != songs[0] {
		t.Fatalf("dynamic favorite %+v %v", p, err)
	}
	p, err = svc.SetRules(ctx, owner, p.ID, nil)
	if err != nil || p.Rules != nil || p.SongCount != 1 || p.Songs[0].ID != songs[0] {
		t.Fatalf("manual recovery %+v %v", p, err)
	}
}

func TestSmartPlaylistRuleValidationAndTime(t *testing.T) {
	valid := model.PlaylistRules{Match: "all", Conditions: []model.PlaylistCondition{{Field: "rating", Operator: "gte", Value: "4"}}, Sort: "title", Limit: 100}
	for _, change := range []func(*model.PlaylistRules){func(r *model.PlaylistRules) { r.Limit = 0 }, func(r *model.PlaylistRules) { r.Sort = "random" }, func(r *model.PlaylistRules) {
		r.Conditions = []model.PlaylistCondition{{Field: "rating", Operator: "gte", Value: "NaN"}}
	}, func(r *model.PlaylistRules) {
		r.Conditions = []model.PlaylistCondition{{Field: "added", Operator: "before", Value: "2026-02-30"}}
	}} {
		rules := valid
		change(&rules)
		if validatePlaylistRules(rules) == nil {
			t.Fatalf("accepted invalid %+v", rules)
		}
	}
	if err := validatePlaylistRules(valid); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	data := playlistRuleData{now: now}
	song := model.Media{CreatedAt: now.Add(-48 * time.Hour), ListeningStats: model.ListeningStats{PersonalPlayCount: 3, LastPlayedAt: &now}}
	for _, tc := range []struct {
		c    model.PlaylistCondition
		want bool
	}{
		{model.PlaylistCondition{Field: "plays", Operator: "gte", Value: "3"}, true},
		{model.PlaylistCondition{Field: "added", Operator: "before", Value: "2026-10-22"}, true},
		{model.PlaylistCondition{Field: "added", Operator: "after", Value: "2026-10-21"}, false},
		{model.PlaylistCondition{Field: "added", Operator: "within_days", Value: "3"}, true},
		{model.PlaylistCondition{Field: "last_played", Operator: "within_days", Value: "1"}, true},
	} {
		if got := playlistRuleMatches(song, tc.c, data); got != tc.want {
			t.Errorf("%+v got %v", tc.c, got)
		}
	}
	song.LastPlayedAt = nil
	if playlistRuleMatches(song, model.PlaylistCondition{Field: "last_played", Operator: "before", Value: "2030-01-01"}, data) {
		t.Fatal("never played matched date")
	}
}
