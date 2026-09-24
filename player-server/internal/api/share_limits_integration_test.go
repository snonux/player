package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
	"codeberg.org/snonux/player/internal/service"
)

func TestPublicShareRangeConsumesLimitedUse(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	uid, err := store.CreateUser(ctx, &model.User{Username: "owner", PasswordHash: "h", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	sid, err := store.CreateSet(ctx, &model.Set{Name: "media", RootPath: "/media", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	path := makeTempFile(t, "shared media")
	mid, err := store.CreateMedia(ctx, &model.Media{SetID: sid, RelPath: "a.mp4", FileName: "a.mp4", AbsPath: path, Type: model.MediaTypeVideo, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	limit := 1
	if err := store.CreateShare(ctx, &model.Share{Token: "limited", MediaID: mid, CreatedBy: uid, CreatedAt: now, ExpiresAt: now.Add(time.Hour), MaxUses: &limit}); err != nil {
		t.Fatal(err)
	}

	shareSvc := service.NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil)
	srv := newTestServer(t, store, nil, nil, &internal.Config{SessionTimeoutHours: 24}, nil, nil, shareSvc, nil, nil, nil, nil, nil, nil, nil)
	request := func(path, byteRange string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if byteRange != "" {
			req.Header.Set("Range", byteRange)
		}
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, req)
		if cache := rr.Header().Get("Cache-Control"); cache != "no-store" {
			t.Fatalf("%s cache policy = %q", path, cache)
		}
		return rr
	}

	if rr := request("/s/limited/stream", "bytes=0-3"); rr.Code != http.StatusPartialContent || rr.Body.String() != "shar" {
		t.Fatalf("first range = %d %q", rr.Code, rr.Body.String())
	}
	for _, path := range []string{"/s/limited/stream", "/s/limited/download", "/s/limited", "/s/limited/thumbnail"} {
		if rr := request(path, ""); rr.Code != http.StatusGone {
			t.Fatalf("%s = %d, want 410", path, rr.Code)
		}
	}
	share, err := store.GetShareByToken(ctx, "limited")
	if err != nil || share.UsedCount != 1 {
		t.Fatalf("used count = %+v, err=%v", share, err)
	}
}
