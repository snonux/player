package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/radio"
	"codeberg.org/snonux/player/internal/repository"
)

// Radio errors. Upstream detail (station addresses, directory answers) is
// logged, never sent to the client.
var (
	ErrInvalidRadioQuery = &apiError{msg: "invalid radio query", status: http.StatusBadRequest}
	ErrRadioNotFound     = &apiError{msg: "radio station not found", status: http.StatusNotFound}
	ErrRadioUpstream     = &apiError{msg: "radio station or directory unreachable", status: http.StatusBadGateway}
	ErrRadioNotAudio     = &apiError{msg: "radio station did not send audio", status: http.StatusUnsupportedMediaType}
	ErrRadioBusy         = &apiError{msg: "too many radio streams", status: http.StatusServiceUnavailable}
)

// Radio limits.
const (
	RadioMaxSearchLimit     = 100
	RadioDefaultSearchLimit = 50
	RadioMaxOffset          = 10000
	RadioMaxTextRunes       = 100
	RadioDefaultTagLimit    = 60
	RadioMaxTagLimit        = 200
	// RadioStreamsPerUser bounds the relayed streams one user can hold
	// open (a browser tab, the phone, and one in between while switching).
	RadioStreamsPerUser = 3
	// RadioStreamsTotal bounds all relayed streams of the server.
	RadioStreamsTotal = 64
	// radioStationCacheSize bounds the stations remembered from searches.
	radioStationCacheSize = 5000
	radioStationCacheTTL  = 6 * time.Hour
	radioListCacheTTL     = time.Hour
)

// RadioService searches the station directory, keeps the users' station
// bookmarks and opens the streams the server relays.
type RadioService interface {
	SearchRadioStations(ctx context.Context, userID int64, q model.RadioQuery) ([]model.RadioStation, error)
	ListRadioTags(ctx context.Context, limit int) ([]model.RadioTag, error)
	ListRadioCountries(ctx context.Context) ([]model.RadioCountry, error)
	ListRadioBookmarks(ctx context.Context, userID int64) ([]model.RadioStation, error)
	AddRadioBookmark(ctx context.Context, userID int64, uuid string) (*model.RadioStation, error)
	RemoveRadioBookmark(ctx context.Context, userID int64, uuid string) error
	// RadioStation resolves a station the user can play.
	RadioStation(ctx context.Context, userID int64, uuid string) (*model.RadioStation, error)
	// OpenRadioStream connects to a station. The caller must Close the
	// stream, which also frees the user's stream slot.
	OpenRadioStream(ctx context.Context, userID int64, uuid string) (*RadioStream, error)
	OpenRadioFavicon(ctx context.Context, userID int64, uuid string) (*RadioStream, error)
}

// RadioFetcher opens station streams and favicons (radio.Fetcher).
type RadioFetcher interface {
	OpenStream(ctx context.Context, streamURL, fallbackType string) (*radio.Response, error)
	OpenFavicon(ctx context.Context, faviconURL string) (*radio.Response, error)
}

// RadioStream is an open relayed answer.
type RadioStream struct {
	ContentType string
	Body        io.ReadCloser
	release     func()
	once        sync.Once
}

// Close closes the upstream connection and frees the stream slot.
func (s *RadioStream) Close() error {
	err := s.Body.Close()
	s.once.Do(func() {
		if s.release != nil {
			s.release()
		}
	})
	return err
}

type cachedStation struct {
	station model.RadioStation
	expires time.Time
}

type cachedList[T any] struct {
	items   []T
	key     int
	expires time.Time
}

type radioService struct {
	store   repository.RadioRepo
	dir     radio.Directory
	fetch   RadioFetcher
	clock   clock.Clock
	logger  *slog.Logger
	baseCtx context.Context

	mu        sync.Mutex
	stations  map[string]cachedStation
	tags      cachedList[model.RadioTag]
	countries cachedList[model.RadioCountry]
	perUser   map[int64]int
	total     int
}

// NewRadioService wires the radio feature. baseCtx bounds background work
// (click counting) to the application's lifetime.
func NewRadioService(store repository.RadioRepo, dir radio.Directory, fetch RadioFetcher, clk clock.Clock, logger *slog.Logger, baseCtx context.Context) RadioService {
	if logger == nil {
		logger = slog.Default()
	}
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	return &radioService{
		store: store, dir: dir, fetch: fetch, clock: clk, logger: logger, baseCtx: baseCtx,
		stations: map[string]cachedStation{},
		perUser:  map[int64]int{},
	}
}

// NormalizeRadioQuery validates a search and fills in the defaults.
func NormalizeRadioQuery(q model.RadioQuery) (model.RadioQuery, error) {
	q.Name = strings.TrimSpace(q.Name)
	q.Tag = strings.ToLower(strings.TrimSpace(q.Tag))
	q.CountryCode = strings.ToUpper(strings.TrimSpace(q.CountryCode))
	for _, text := range []string{q.Name, q.Tag} {
		if !utf8.ValidString(text) || utf8.RuneCountInString(text) > RadioMaxTextRunes {
			return q, ErrInvalidRadioQuery
		}
	}
	if q.CountryCode != "" && !model.ValidRadioCountryCode(q.CountryCode) {
		return q, ErrInvalidRadioQuery
	}
	switch q.Order {
	case "":
		q.Order = model.RadioOrderPopular
	case model.RadioOrderPopular, model.RadioOrderVotes, model.RadioOrderName, model.RadioOrderRandom:
	default:
		return q, ErrInvalidRadioQuery
	}
	if q.Limit == 0 {
		q.Limit = RadioDefaultSearchLimit
	}
	if q.Limit < 1 || q.Limit > RadioMaxSearchLimit || q.Offset < 0 || q.Offset > RadioMaxOffset {
		return q, ErrInvalidRadioQuery
	}
	return q, nil
}

func (s *radioService) SearchRadioStations(ctx context.Context, userID int64, q model.RadioQuery) ([]model.RadioStation, error) {
	q, err := NormalizeRadioQuery(q)
	if err != nil {
		return nil, err
	}
	stations, err := s.dir.Search(ctx, q)
	if err != nil {
		return nil, s.upstream("search radio stations", err)
	}
	s.remember(stations)
	bookmarks, err := s.bookmarkTimes(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range stations {
		if at, ok := bookmarks[stations[i].UUID]; ok {
			at := at
			stations[i].BookmarkedAt = &at
		}
	}
	return stations, nil
}

func (s *radioService) bookmarkTimes(ctx context.Context, userID int64) (map[string]time.Time, error) {
	bookmarks, err := s.store.ListRadioBookmarks(ctx, userID)
	if err != nil {
		return nil, err
	}
	times := make(map[string]time.Time, len(bookmarks))
	for _, b := range bookmarks {
		times[b.Station.UUID] = b.CreatedAt
	}
	return times, nil
}

func (s *radioService) ListRadioTags(ctx context.Context, limit int) ([]model.RadioTag, error) {
	if limit == 0 {
		limit = RadioDefaultTagLimit
	}
	if limit < 1 || limit > RadioMaxTagLimit {
		return nil, ErrInvalidRadioQuery
	}
	return cachedFetch(s, &s.tags, limit, func() ([]model.RadioTag, error) { return s.dir.Tags(ctx, limit) }, "list radio tags")
}

func (s *radioService) ListRadioCountries(ctx context.Context) ([]model.RadioCountry, error) {
	return cachedFetch(s, &s.countries, 0, func() ([]model.RadioCountry, error) { return s.dir.Countries(ctx) }, "list radio countries")
}

// cachedFetch answers from a list cache for an hour: tags and countries
// change slowly and the explorer asks for them every time it opens.
func cachedFetch[T any](s *radioService, cache *cachedList[T], key int, load func() ([]T, error), what string) ([]T, error) {
	now := s.clock.Now()
	s.mu.Lock()
	if cache.items != nil && cache.key == key && now.Before(cache.expires) {
		items := cache.items
		s.mu.Unlock()
		return items, nil
	}
	s.mu.Unlock()
	items, err := load()
	if err != nil {
		return nil, s.upstream(what, err)
	}
	s.mu.Lock()
	*cache = cachedList[T]{items: items, key: key, expires: now.Add(radioListCacheTTL)}
	s.mu.Unlock()
	return items, nil
}

func (s *radioService) ListRadioBookmarks(ctx context.Context, userID int64) ([]model.RadioStation, error) {
	bookmarks, err := s.store.ListRadioBookmarks(ctx, userID)
	if err != nil {
		return nil, err
	}
	stations := make([]model.RadioStation, 0, len(bookmarks))
	for _, b := range bookmarks {
		stations = append(stations, bookmarkedStation(b))
	}
	return stations, nil
}

func bookmarkedStation(b model.RadioBookmark) model.RadioStation {
	st := b.Station
	at := b.CreatedAt
	st.BookmarkedAt = &at
	return st
}

func (s *radioService) AddRadioBookmark(ctx context.Context, userID int64, uuid string) (*model.RadioStation, error) {
	if !model.ValidRadioUUID(uuid) {
		return nil, ErrRadioNotFound
	}
	existing, err := s.store.GetRadioBookmark(ctx, userID, uuid)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		st := bookmarkedStation(*existing)
		return &st, nil
	}
	st, err := s.lookup(ctx, uuid)
	if err != nil {
		return nil, err
	}
	saved, err := s.store.UpsertRadioBookmark(ctx, &model.RadioBookmark{UserID: userID, Station: *st, CreatedAt: s.clock.Now().UTC()})
	if err != nil {
		return nil, err
	}
	result := bookmarkedStation(*saved)
	return &result, nil
}

func (s *radioService) RemoveRadioBookmark(ctx context.Context, userID int64, uuid string) error {
	if !model.ValidRadioUUID(uuid) {
		return ErrRadioNotFound
	}
	return s.store.DeleteRadioBookmark(ctx, userID, uuid)
}

// RadioStation resolves a station from the user's bookmark (which works
// without the directory), the stations seen in recent searches, or the
// directory, in that order.
func (s *radioService) RadioStation(ctx context.Context, userID int64, uuid string) (*model.RadioStation, error) {
	if !model.ValidRadioUUID(uuid) {
		return nil, ErrRadioNotFound
	}
	b, err := s.store.GetRadioBookmark(ctx, userID, uuid)
	if err != nil {
		return nil, err
	}
	if b != nil {
		st := bookmarkedStation(*b)
		return &st, nil
	}
	return s.lookup(ctx, uuid)
}

// lookup finds a station in the search cache or the directory.
func (s *radioService) lookup(ctx context.Context, uuid string) (*model.RadioStation, error) {
	now := s.clock.Now()
	s.mu.Lock()
	cached, ok := s.stations[uuid]
	s.mu.Unlock()
	if ok && now.Before(cached.expires) {
		st := cached.station
		return &st, nil
	}
	st, err := s.dir.Station(ctx, uuid)
	if errors.Is(err, radio.ErrStationNotFound) {
		return nil, ErrRadioNotFound
	}
	if err != nil {
		return nil, s.upstream("look up radio station", err)
	}
	s.remember([]model.RadioStation{*st})
	return st, nil
}

// remember caches stations seen in directory answers, so playing or
// bookmarking a station found by a search needs no further lookup.
func (s *radioService) remember(stations []model.RadioStation) {
	now := s.clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stations)+len(stations) > radioStationCacheSize {
		for uuid, c := range s.stations {
			if !now.Before(c.expires) {
				delete(s.stations, uuid)
			}
		}
	}
	if len(s.stations)+len(stations) > radioStationCacheSize {
		// Still full: start over rather than track recency. Evicted
		// stations are simply looked up in the directory again.
		s.stations = map[string]cachedStation{}
	}
	for _, st := range stations {
		st.BookmarkedAt = nil
		s.stations[st.UUID] = cachedStation{station: st, expires: now.Add(radioStationCacheTTL)}
	}
}

func (s *radioService) OpenRadioStream(ctx context.Context, userID int64, uuid string) (*RadioStream, error) {
	st, err := s.RadioStation(ctx, userID, uuid)
	if err != nil {
		return nil, err
	}
	release, err := s.acquire(userID)
	if err != nil {
		return nil, err
	}
	resp, err := s.fetch.OpenStream(ctx, st.StreamURL, radio.CodecContentType(st.Codec))
	if err != nil {
		release()
		if errors.Is(err, radio.ErrNotAudio) {
			s.logger.Warn("radio station did not send audio", "station", uuid, "err", err)
			return nil, ErrRadioNotAudio
		}
		return nil, s.upstream("open radio stream", err)
	}
	go s.countClick(uuid)
	return &RadioStream{ContentType: resp.ContentType, Body: resp.Body, release: release}, nil
}

func (s *radioService) countClick(uuid string) {
	ctx, cancel := context.WithTimeout(s.baseCtx, 10*time.Second)
	defer cancel()
	s.dir.CountClick(ctx, uuid)
}

func (s *radioService) OpenRadioFavicon(ctx context.Context, userID int64, uuid string) (*RadioStream, error) {
	st, err := s.RadioStation(ctx, userID, uuid)
	if err != nil {
		return nil, err
	}
	if st.FaviconURL == "" {
		return nil, ErrRadioNotFound
	}
	resp, err := s.fetch.OpenFavicon(ctx, st.FaviconURL)
	if err != nil {
		s.logger.Debug("radio favicon unavailable", "station", uuid, "err", err)
		return nil, ErrRadioNotFound
	}
	return &RadioStream{ContentType: resp.ContentType, Body: resp.Body}, nil
}

// acquire takes one of the user's stream slots.
func (s *radioService) acquire(userID int64) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perUser[userID] >= RadioStreamsPerUser || s.total >= RadioStreamsTotal {
		return nil, ErrRadioBusy
	}
	s.perUser[userID]++
	s.total++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.total--
		if s.perUser[userID]--; s.perUser[userID] <= 0 {
			delete(s.perUser, userID)
		}
	}, nil
}

// upstream logs a directory or station failure and returns the error the
// client sees. A cancelled request is the client's doing and not logged.
func (s *radioService) upstream(what string, err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	s.logger.Warn(what+" failed", "err", err)
	return ErrRadioUpstream
}
