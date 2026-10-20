package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"codeberg.org/snonux/player/internal"
)

const (
	radioStationUUID = "11111111-2222-3333-4444-555555555555"
	radioHLSUUID     = "aaaaaaaa-2222-3333-4444-555555555555"
)

// fakeRadioBrowser serves the parts of the Radio Browser API the server
// uses, plus a station stream and favicon.
type fakeRadioBrowser struct {
	server     *httptest.Server
	lookups    atomic.Int32
	clicks     atomic.Int32
	lastSearch atomic.Value
}

func newFakeRadioBrowser(t *testing.T) *fakeRadioBrowser {
	t.Helper()
	f := &fakeRadioBrowser{}
	mux := http.NewServeMux()
	stations := func() []map[string]any {
		base := f.server.URL
		return []map[string]any{
			{"stationuuid": radioStationUUID, "name": "Test Tone FM", "url": base + "/playlist.pls", "url_resolved": base + "/live",
				"homepage": "https://example.org/", "favicon": base + "/icon.png", "tags": "jazz,smooth,jazz", "country": "Germany",
				"countrycode": "de", "language": "german", "codec": "MP3", "bitrate": 128, "votes": 7, "hls": 0},
			{"stationuuid": radioHLSUUID, "name": "HLS only", "url_resolved": base + "/live.m3u8", "hls": 1},
		}
	}
	mux.HandleFunc("GET /json/stations/search", func(w http.ResponseWriter, r *http.Request) {
		f.lastSearch.Store(r.URL.Query())
		_ = json.NewEncoder(w).Encode(stations())
	})
	mux.HandleFunc("GET /json/stations/byuuid/{uuid}", func(w http.ResponseWriter, r *http.Request) {
		f.lookups.Add(1)
		found := []map[string]any{}
		for _, s := range stations() {
			if s["stationuuid"] == r.PathValue("uuid") {
				found = append(found, s)
			}
		}
		_ = json.NewEncoder(w).Encode(found)
	})
	mux.HandleFunc("GET /json/tags", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"jazz","stationcount":12},{"name":"","stationcount":3}]`))
	})
	mux.HandleFunc("GET /json/countries", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"Germany","iso_3166_1":"DE","stationcount":9},{"name":"Deutschland","iso_3166_1":"DE","stationcount":1},{"name":"Nowhere","iso_3166_1":"","stationcount":5}]`))
	})
	mux.HandleFunc("GET /json/url/{uuid}", func(w http.ResponseWriter, _ *http.Request) {
		f.clicks.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("GET /live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte(strings.Repeat("ID3audio", 1000)))
	})
	mux.HandleFunc("GET /icon.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG fake"))
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func newRadioE2E(t *testing.T, allowPrivate bool) (*e2e, *fakeRadioBrowser) {
	t.Helper()
	fake := newFakeRadioBrowser(t)
	e := newE2EWith(t, &fakeRunner{}, func(cfg *internal.Config) {
		cfg.RadioBrowserURL = fake.server.URL
		cfg.RadioAllowPrivateNetworks = allowPrivate
	})
	return e, fake
}

type radioStation struct {
	UUID         string   `json:"uuid"`
	Name         string   `json:"name"`
	FaviconURL   string   `json:"favicon_url"`
	StreamURL    string   `json:"stream_url"`
	CountryCode  string   `json:"country_code"`
	Tags         []string `json:"tags"`
	Bitrate      int      `json:"bitrate"`
	Bookmarked   bool     `json:"bookmarked"`
	BookmarkedAt *string  `json:"bookmarked_at"`
}

func decodeStations(t *testing.T, rr *httptest.ResponseRecorder) []radioStation {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rr.Code, rr.Body)
	}
	var stations []radioStation
	if err := json.Unmarshal(rr.Body.Bytes(), &stations); err != nil || stations == nil {
		t.Fatalf("decode %s: %v", rr.Body, err)
	}
	return stations
}

func TestRadio_SearchBookmarkAndRelay(t *testing.T) {
	e, fake := newRadioE2E(t, true)
	ctx := context.Background()

	if rr := e.get("/api/radio/stations", ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous search = %d", rr.Code)
	}
	stations := decodeStations(t, e.get("/api/v1/radio/stations?q=tone&tag=Jazz&country=de&order=votes&limit=10&offset=5", "alice"))
	if len(stations) != 1 {
		t.Fatalf("HLS station not filtered: %+v", stations)
	}
	st := stations[0]
	base := "/api/radio/stations/" + radioStationUUID
	if st.UUID != radioStationUUID || st.StreamURL != base+"/stream" || st.FaviconURL != base+"/favicon" ||
		st.CountryCode != "DE" || strings.Join(st.Tags, ",") != "jazz,smooth" || st.Bitrate != 128 || st.Bookmarked {
		t.Fatalf("station = %+v", st)
	}
	q := fake.lastSearch.Load().(url.Values)
	for key, want := range map[string]string{"name": "tone", "tag": "jazz", "tagExact": "true", "countrycode": "DE", "order": "votes", "reverse": "true", "limit": "10", "offset": "5", "hidebroken": "true"} {
		if got := q[key]; len(got) != 1 || got[0] != want {
			t.Errorf("directory param %s = %v, want %s", key, got, want)
		}
	}
	if strings.Contains(e.get("/api/radio/stations", "alice").Body.String(), fake.server.URL) {
		t.Fatal("station addresses must not reach the client")
	}
	for _, bad := range []string{"order=loud", "limit=101", "offset=-1", "country=Germany", "limit=x", "q=" + strings.Repeat("a", 101)} {
		if rr := e.get("/api/radio/stations?"+bad, "alice"); rr.Code != http.StatusBadRequest {
			t.Errorf("search %s = %d", bad, rr.Code)
		}
	}

	if rr := e.get("/api/radio/tags", "alice"); rr.Code != 200 || strings.TrimSpace(rr.Body.String()) != `[{"name":"jazz","station_count":12}]` {
		t.Fatalf("tags = %d %s", rr.Code, rr.Body)
	}
	if rr := e.get("/api/radio/countries", "alice"); rr.Code != 200 || strings.TrimSpace(rr.Body.String()) != `[{"code":"DE","name":"Germany","station_count":10}]` {
		t.Fatalf("countries = %d %s", rr.Code, rr.Body)
	}

	// Bookmarks are per user and survive without the directory.
	rr := e.request(ctx, http.MethodPut, "/api/radio/bookmarks/"+radioStationUUID, "alice", "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"bookmarked":true`) {
		t.Fatalf("bookmark = %d %s", rr.Code, rr.Body)
	}
	if rr := e.request(ctx, http.MethodPut, "/api/v1/radio/bookmarks/"+radioStationUUID, "alice", ""); rr.Code != http.StatusOK {
		t.Fatalf("bookmark again = %d %s", rr.Code, rr.Body)
	}
	if fake.lookups.Load() != 0 {
		t.Fatal("a station seen in a search must not be looked up again")
	}
	bookmarks := decodeStations(t, e.get("/api/radio/bookmarks", "alice"))
	if len(bookmarks) != 1 || !bookmarks[0].Bookmarked || bookmarks[0].BookmarkedAt == nil || bookmarks[0].Name != "Test Tone FM" {
		t.Fatalf("bookmarks = %+v", bookmarks)
	}
	if got := decodeStations(t, e.get("/api/radio/bookmarks", "bob")); len(got) != 0 {
		t.Fatalf("bob sees alice's bookmarks: %+v", got)
	}
	if got := decodeStations(t, e.get("/api/radio/stations", "alice")); !got[0].Bookmarked {
		t.Fatal("search does not mark the bookmarked station")
	}
	if got := decodeStations(t, e.get("/api/radio/stations", "bob")); got[0].Bookmarked {
		t.Fatal("search marks another user's bookmark")
	}

	// Relay.
	rr = e.get(base+"/stream", "alice")
	if rr.Code != http.StatusOK || rr.Header().Get("Content-Type") != "audio/mpeg" || rr.Body.Len() != 8000 ||
		rr.Header().Get("Cache-Control") != "no-store" || rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("stream = %d %v len %d", rr.Code, rr.Header(), rr.Body.Len())
	}
	if rr := e.request(ctx, http.MethodHead, "/api/v1"+strings.TrimPrefix(base, "/api")+"/stream", "bob", ""); rr.Code != http.StatusOK || rr.Body.Len() != 0 || rr.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("HEAD stream = %d %v", rr.Code, rr.Header())
	}
	if rr := e.get(base+"/favicon", "alice"); rr.Code != 200 || rr.Header().Get("Content-Type") != "image/png" || rr.Body.String() != "\x89PNG fake" {
		t.Fatalf("favicon = %d %v %q", rr.Code, rr.Header(), rr.Body)
	}
	if rr := e.get("/api/radio/stations/"+radioHLSUUID+"/stream", "alice"); rr.Code != http.StatusNotFound {
		t.Fatalf("HLS stream = %d %s", rr.Code, rr.Body)
	}
	if rr := e.get("/api/radio/stations/not-a-uuid/stream", "alice"); rr.Code != http.StatusNotFound {
		t.Fatalf("bad uuid = %d", rr.Code)
	}

	// Deleting a bookmark is idempotent; deleting the user removes them.
	for i := 0; i < 2; i++ {
		if rr := e.request(ctx, http.MethodDelete, "/api/radio/bookmarks/"+radioStationUUID, "alice", ""); rr.Code != http.StatusNoContent {
			t.Fatalf("delete bookmark = %d %s", rr.Code, rr.Body)
		}
	}
	if got := decodeStations(t, e.get("/api/radio/bookmarks", "alice")); len(got) != 0 {
		t.Fatalf("bookmark not deleted: %+v", got)
	}
	if rr := e.request(ctx, http.MethodPut, "/api/radio/bookmarks/"+radioStationUUID, "bob", ""); rr.Code != http.StatusOK {
		t.Fatalf("bob bookmark = %d", rr.Code)
	}
	e.must(e.store.DeleteUser(ctx, e.users["bob"]))
	if got, err := e.store.ListRadioBookmarks(ctx, e.users["bob"]); err != nil || len(got) != 0 {
		t.Fatalf("deleted user's bookmarks = %+v %v", got, err)
	}
}

// The relay must not let users reach the server's own network through a
// station entry pointing there.
func TestRadio_RelayRefusesPrivateAddressesByDefault(t *testing.T) {
	e, _ := newRadioE2E(t, false)
	if stations := decodeStations(t, e.get("/api/radio/stations", "alice")); len(stations) != 1 {
		t.Fatalf("search failed: %+v", stations)
	}
	if rr := e.get("/api/radio/stations/"+radioStationUUID+"/stream", "alice"); rr.Code != http.StatusBadGateway {
		t.Fatalf("stream on loopback = %d %s", rr.Code, rr.Body)
	}
	if rr := e.get("/api/radio/stations/"+radioStationUUID+"/favicon", "alice"); rr.Code != http.StatusNotFound {
		t.Fatalf("favicon on loopback = %d", rr.Code)
	}
}

func TestRadio_DirectoryDownIsBadGateway(t *testing.T) {
	e := newE2EWith(t, &fakeRunner{}, func(cfg *internal.Config) { cfg.RadioBrowserURL = "http://127.0.0.1:1" })
	for _, path := range []string{"/api/radio/stations", "/api/radio/tags", "/api/radio/countries"} {
		if rr := e.get(path, "alice"); rr.Code != http.StatusBadGateway || strings.Contains(rr.Body.String(), "127.0.0.1") {
			t.Fatalf("%s = %d %s", path, rr.Code, rr.Body)
		}
	}
	if rr := e.request(context.Background(), http.MethodPut, "/api/radio/bookmarks/"+radioStationUUID, "alice", ""); rr.Code != http.StatusBadGateway {
		t.Fatalf("bookmark unknown station = %d", rr.Code)
	}
}
