package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/radio"
	"github.com/snonux/player/internal/repository"
)

const testStationUUID = "11111111-2222-3333-4444-555555555555"

type fakeDirectory struct {
	stations []model.RadioStation
	err      error
	tagCalls int
}

func (d *fakeDirectory) Search(context.Context, model.RadioQuery) ([]model.RadioStation, error) {
	return append([]model.RadioStation(nil), d.stations...), d.err
}

func (d *fakeDirectory) Station(_ context.Context, uuid string) (*model.RadioStation, error) {
	if d.err != nil {
		return nil, d.err
	}
	for _, s := range d.stations {
		if s.UUID == uuid {
			return &s, nil
		}
	}
	return nil, radio.ErrStationNotFound
}

func (d *fakeDirectory) Tags(context.Context, int) ([]model.RadioTag, error) {
	d.tagCalls++
	return []model.RadioTag{{Name: "jazz", StationCount: 1}}, d.err
}

func (d *fakeDirectory) Countries(context.Context) ([]model.RadioCountry, error) { return nil, d.err }
func (d *fakeDirectory) CountClick(context.Context, string)                      {}

type fakeRadioFetcher struct{ urls []string }

func (f *fakeRadioFetcher) OpenStream(_ context.Context, streamURL, fallback string) (*radio.Response, error) {
	f.urls = append(f.urls, streamURL)
	return &radio.Response{ContentType: fallback, Body: io.NopCloser(strings.NewReader("audio"))}, nil
}

func (f *fakeRadioFetcher) OpenFavicon(context.Context, string) (*radio.Response, error) {
	return nil, radio.ErrNotImage
}

type memoryRadioRepo struct {
	repository.MockRadioRepo
	saved map[string]model.RadioBookmark
}

func newMemoryRadioRepo() *memoryRadioRepo {
	r := &memoryRadioRepo{saved: map[string]model.RadioBookmark{}}
	r.UpsertRadioBookmarkFunc = func(_ context.Context, b *model.RadioBookmark) (*model.RadioBookmark, error) {
		r.saved[b.Station.UUID] = *b
		return b, nil
	}
	r.GetRadioBookmarkFunc = func(_ context.Context, _ int64, uuid string) (*model.RadioBookmark, error) {
		if b, ok := r.saved[uuid]; ok {
			return &b, nil
		}
		return nil, nil
	}
	return r
}

func TestRadioService_StreamSlotsPerUser(t *testing.T) {
	dir := &fakeDirectory{stations: []model.RadioStation{{UUID: testStationUUID, StreamURL: "http://station/live", Codec: "AAC"}}}
	fetch := &fakeRadioFetcher{}
	svc := NewRadioService(newMemoryRadioRepo(), dir, fetch, clock.RealClock{}, nil, nil)
	ctx := context.Background()
	var open []*RadioStream
	for i := 0; i < RadioStreamsPerUser; i++ {
		s, err := svc.OpenRadioStream(ctx, 1, testStationUUID)
		if err != nil {
			t.Fatal(err)
		}
		if s.ContentType != "audio/aac" {
			t.Fatalf("content type = %s", s.ContentType)
		}
		open = append(open, s)
	}
	if _, err := svc.OpenRadioStream(ctx, 1, testStationUUID); !errors.Is(err, ErrRadioBusy) {
		t.Fatalf("stream over the limit: err = %v", err)
	}
	if s, err := svc.OpenRadioStream(ctx, 2, testStationUUID); err != nil {
		t.Fatalf("other user is limited too: %v", err)
	} else {
		s.Close()
	}
	// Closing twice frees one slot only once.
	open[0].Close()
	open[0].Close()
	s, err := svc.OpenRadioStream(ctx, 1, testStationUUID)
	if err != nil {
		t.Fatalf("slot not freed: %v", err)
	}
	if _, err := svc.OpenRadioStream(ctx, 1, testStationUUID); !errors.Is(err, ErrRadioBusy) {
		t.Fatalf("double close freed two slots: %v", err)
	}
	s.Close()
}

func TestRadioService_BookmarkPlaysWithoutDirectory(t *testing.T) {
	dir := &fakeDirectory{stations: []model.RadioStation{{UUID: testStationUUID, Name: "Jazz", StreamURL: "http://station/live"}}}
	fetch := &fakeRadioFetcher{}
	clk := &clock.MockClock{T: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	svc := NewRadioService(newMemoryRadioRepo(), dir, fetch, clk, nil, nil)
	ctx := context.Background()
	st, err := svc.AddRadioBookmark(ctx, 1, testStationUUID)
	if err != nil || st.BookmarkedAt == nil || !st.BookmarkedAt.Equal(clk.Now()) {
		t.Fatalf("bookmark = %+v %v", st, err)
	}
	dir.err = radio.ErrUnavailable
	dir.stations = nil
	s, err := svc.OpenRadioStream(ctx, 1, testStationUUID)
	if err != nil {
		t.Fatalf("bookmarked station needs the directory: %v", err)
	}
	s.Close()
	if _, err := svc.AddRadioBookmark(ctx, 1, "22222222-2222-3333-4444-555555555555"); !errors.Is(err, ErrRadioUpstream) {
		t.Fatalf("unknown station with directory down: %v", err)
	}
	if _, err := svc.OpenRadioFavicon(ctx, 1, testStationUUID); !errors.Is(err, ErrRadioNotFound) {
		t.Fatalf("station without favicon: %v", err)
	}
}

func TestRadioService_TagsAreCached(t *testing.T) {
	dir := &fakeDirectory{}
	clk := &clock.MockClock{T: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	svc := NewRadioService(newMemoryRadioRepo(), dir, &fakeRadioFetcher{}, clk, nil, nil)
	for i := 0; i < 3; i++ {
		if _, err := svc.ListRadioTags(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
	}
	if dir.tagCalls != 1 {
		t.Fatalf("directory asked %d times", dir.tagCalls)
	}
	clk.T = clk.T.Add(radioListCacheTTL + time.Second)
	if _, err := svc.ListRadioTags(context.Background(), 0); err != nil || dir.tagCalls != 2 {
		t.Fatalf("expired cache not refreshed: %d %v", dir.tagCalls, err)
	}
	if _, err := svc.ListRadioTags(context.Background(), RadioMaxTagLimit+1); !errors.Is(err, ErrInvalidRadioQuery) {
		t.Fatalf("limit not checked: %v", err)
	}
}
