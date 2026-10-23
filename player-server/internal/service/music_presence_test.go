package service

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

type presenceCountingSource struct {
	CatalogSource
	calls map[int64]int
}

func (s presenceCountingSource) ListMedia(ctx context.Context, user int64, filter MediaQueryFilter) ([]model.Media, error) {
	s.calls[user]++
	return s.CatalogSource.ListMedia(ctx, user, filter)
}

func TestMusicPresencePrivacyLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	clk := &clock.MockClock{T: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	publisher, err := store.CreateUser(ctx, &model.User{Username: "publisher", PasswordHash: "x", CreatedAt: clk.Now()})
	check(err)
	viewer, err := store.CreateUser(ctx, &model.User{Username: "viewer", PasswordHash: "x", CreatedAt: clk.Now()})
	check(err)
	set, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "music", CreatedAt: clk.Now()})
	check(err)
	song, err := store.CreateMedia(ctx, &model.Media{SetID: set, FileName: "private.mp3", RelPath: "private.mp3", Type: model.MediaTypeAudio, Duration: 120, CreatedAt: clk.Now(), Music: &model.MusicMetadata{Title: "Private title"}})
	check(err)
	for _, user := range []int64{publisher, viewer} {
		check(store.GrantPermission(ctx, &model.SetPermission{SetID: set, UserID: user, Role: model.RoleViewer}))
	}
	session := model.Session{ID: "publisher-session", UserID: publisher, ExpiresAt: clk.Now().Add(time.Hour), CreatedAt: clk.Now()}
	check(store.CreateSession(ctx, &session))
	source := presenceCountingSource{CatalogSource: NewMediaService(store, clk, "", nil, nil), calls: map[int64]int{}}
	svc := NewMusicPresenceService(store, source, clk)
	update := PresenceUpdate{ClientID: "client-1234567890", Client: "web", MediaID: song, State: "playing", Position: 140, Sequence: 1}
	listed := func(want int) {
		t.Helper()
		rows, err := svc.List(ctx, viewer)
		check(err)
		if len(rows) != want {
			t.Fatalf("rows %+v want %d", rows, want)
		}
		if len(rows) > 0 && (rows[0].Title != "Private title" || rows[0].Position != 120) {
			t.Fatalf("fresh metadata/clamp %+v", rows)
		}
	}
	listed(0)
	check(svc.Update(ctx, session, update))
	second := update
	second.ClientID = "client-second-123456"
	check(svc.Update(ctx, session, second))
	clear(source.calls)
	rows, err := svc.List(ctx, viewer)
	check(err)
	if len(rows) != 2 || source.calls[publisher] != 1 || source.calls[viewer] != 1 {
		t.Fatalf("request-local catalog reuse rows=%d calls=%v", len(rows), source.calls)
	}
	check(svc.Remove(session, second.ClientID, 2))
	listed(1)
	check(store.RevokePermission(ctx, set, viewer))
	listed(0)
	check(store.GrantPermission(ctx, &model.SetPermission{SetID: set, UserID: viewer, Role: model.RoleViewer}))
	listed(1)
	check(store.RevokePermission(ctx, set, publisher))
	listed(0)
	check(store.GrantPermission(ctx, &model.SetPermission{SetID: set, UserID: publisher, Role: model.RoleViewer}))
	listed(0)
	update.Sequence = 2
	check(svc.Update(ctx, session, update))
	listed(1)
	check(svc.Remove(session, update.ClientID, 3))
	update.Sequence = 2
	check(svc.Update(ctx, session, update))
	listed(0)
	update.Sequence = 4
	check(svc.Update(ctx, session, update))
	listed(1)
	check(store.DeleteSession(ctx, session.ID))
	listed(0)
	check(store.CreateSession(ctx, &session))
	listed(0)
	update.Sequence = 5
	check(svc.Update(ctx, session, update))
	listed(1)
	clk.T = clk.T.Add(presenceTTL)
	listed(0)
	for _, position := range []float64{-1, math.NaN(), math.Inf(1)} {
		update.Position = position
		if svc.Update(ctx, session, update) == nil {
			t.Fatalf("accepted position %v", position)
		}
	}
	update.Position = 1
	update.ClientID = "bad"
	if svc.Update(ctx, session, update) == nil {
		t.Fatal("accepted client")
	}
}

func TestMusicPresenceTokenRevocationAndCaps(t *testing.T) {
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	clk := &clock.MockClock{T: time.Now().UTC()}
	user, err := store.CreateUser(ctx, &model.User{Username: "u", PasswordHash: "x", CreatedAt: clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.Create(ctx, &model.APIToken{UserID: user, Name: "native", TokenHash: "hash", CreatedAt: clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewMusicPresenceService(store, albumSource{}, clk)
	session := model.Session{ID: "api-token:" + strconv.FormatInt(token, 10), UserID: user}
	valid, err := svc.validSession(ctx, session)
	if err != nil || !valid {
		t.Fatalf("valid %v %v", valid, err)
	}
	if err := store.DeleteByID(ctx, token); err != nil {
		t.Fatal(err)
	}
	valid, err = svc.validSession(ctx, session)
	if err != nil || valid {
		t.Fatalf("revoked %v %v", valid, err)
	}
	for i := int64(0); i < 8; i++ {
		if err := svc.Remove(session, "client-123456789"+strconv.FormatInt(i, 10), i+1); err != nil {
			t.Fatal(err)
		}
	}
	if svc.Remove(session, "client-1234567899", 10) == nil {
		t.Fatal("accepted ninth client")
	}
	clk.T = clk.T.Add(presenceTTL)
	if err := svc.Remove(session, "client-1234567899", 10); err != nil {
		t.Fatal(err)
	}
}
