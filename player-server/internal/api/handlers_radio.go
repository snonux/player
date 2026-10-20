package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/radio"
	"codeberg.org/snonux/player/internal/service"
)

// radioIdleTimeout ends a relay when the station sends nothing for this
// long, and is also the time each write to the client may take.
const radioIdleTimeout = 30 * time.Second

// radioStationJSON is a station as clients see it: stream and favicon are
// relay paths on this server, never the station's own addresses.
type radioStationJSON struct {
	UUID         string     `json:"uuid"`
	Name         string     `json:"name"`
	Homepage     string     `json:"homepage"`
	FaviconURL   string     `json:"favicon_url"`
	StreamURL    string     `json:"stream_url"`
	Country      string     `json:"country"`
	CountryCode  string     `json:"country_code"`
	Language     string     `json:"language"`
	Tags         []string   `json:"tags"`
	Codec        string     `json:"codec"`
	Bitrate      int        `json:"bitrate"`
	Votes        int        `json:"votes"`
	Bookmarked   bool       `json:"bookmarked"`
	BookmarkedAt *time.Time `json:"bookmarked_at,omitempty"`
}

func radioStationResponse(st model.RadioStation) radioStationJSON {
	base := "/api/radio/stations/" + url.PathEscape(st.UUID)
	favicon := ""
	if st.FaviconURL != "" {
		favicon = base + "/favicon"
	}
	tags := st.Tags
	if tags == nil {
		tags = []string{}
	}
	return radioStationJSON{
		UUID: st.UUID, Name: st.Name, Homepage: st.Homepage, FaviconURL: favicon, StreamURL: base + "/stream",
		Country: st.Country, CountryCode: st.CountryCode, Language: st.Language, Tags: tags,
		Codec: st.Codec, Bitrate: st.Bitrate, Votes: st.Votes,
		Bookmarked: st.BookmarkedAt != nil, BookmarkedAt: st.BookmarkedAt,
	}
}

func radioStationsResponse(stations []model.RadioStation) []radioStationJSON {
	out := make([]radioStationJSON, 0, len(stations))
	for _, st := range stations {
		out = append(out, radioStationResponse(st))
	}
	return out
}

// routesRadio wires the web radio routes.
func (s *Server) routesRadio() {
	s.handleBoth(http.MethodGet, "/api/radio/stations", s.requireSession(s.handleRadioSearch))
	s.handleBoth(http.MethodGet, "/api/radio/tags", s.requireSession(s.handleRadioTags))
	s.handleBoth(http.MethodGet, "/api/radio/countries", s.requireSession(s.handleRadioCountries))
	s.handleBoth(http.MethodGet, "/api/radio/stations/{uuid}/stream", s.requireSession(s.handleRadioStream))
	s.handleBoth(http.MethodGet, "/api/radio/stations/{uuid}/favicon", s.requireSession(s.handleRadioFavicon))
	s.handleBoth(http.MethodGet, "/api/radio/bookmarks", s.requireSession(s.handleRadioBookmarks))
	s.handleBoth(http.MethodPut, "/api/radio/bookmarks/{uuid}", s.requireSession(s.handleRadioAddBookmark))
	s.handleBoth(http.MethodDelete, "/api/radio/bookmarks/{uuid}", s.requireSession(s.handleRadioRemoveBookmark))
}

// queryInt reads an optional integer query parameter.
func queryInt(r *http.Request, name string) (int, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, true
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

func (s *Server) handleRadioSearch(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.radio) {
		return
	}
	limit, okLimit := queryInt(r, "limit")
	offset, okOffset := queryInt(r, "offset")
	if !okLimit || !okOffset {
		badRequest(w, "limit and offset must be integers")
		return
	}
	query := r.URL.Query()
	stations, err := s.radio.SearchRadioStations(r.Context(), userIDFromContext(r), model.RadioQuery{
		Name: query.Get("q"), Tag: query.Get("tag"), CountryCode: query.Get("country"),
		Order: query.Get("order"), Limit: limit, Offset: offset,
	})
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, radioStationsResponse(stations))
}

func (s *Server) handleRadioTags(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.radio) {
		return
	}
	limit, ok := queryInt(r, "limit")
	if !ok {
		badRequest(w, "limit must be an integer")
		return
	}
	tags, err := s.radio.ListRadioTags(r.Context(), limit)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jsonArray(tags))
}

func (s *Server) handleRadioCountries(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.radio) {
		return
	}
	countries, err := s.radio.ListRadioCountries(r.Context())
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jsonArray(countries))
}

func (s *Server) handleRadioBookmarks(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.radio) {
		return
	}
	stations, err := s.radio.ListRadioBookmarks(r.Context(), userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, radioStationsResponse(stations))
}

func (s *Server) handleRadioAddBookmark(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.radio) {
		return
	}
	st, err := s.radio.AddRadioBookmark(r.Context(), userIDFromContext(r), r.PathValue("uuid"))
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, radioStationResponse(*st))
}

func (s *Server) handleRadioRemoveBookmark(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.radio) {
		return
	}
	if err := s.radio.RemoveRadioBookmark(r.Context(), userIDFromContext(r), r.PathValue("uuid")); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setRelayHeaders marks a relayed answer as foreign content: never cached
// as ours, never sniffed into something executable, and sandboxed should a
// browser render it as a document.
func setRelayHeaders(w http.ResponseWriter, contentType string) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
}

// handleRadioStream relays a live station stream. HEAD answers from the
// station data without connecting to the station.
func (s *Server) handleRadioStream(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.radio) {
		return
	}
	uuid := r.PathValue("uuid")
	userID := userIDFromContext(r)
	if r.Method == http.MethodHead {
		st, err := s.radio.RadioStation(r.Context(), userID, uuid)
		if err != nil {
			handleError(w, err)
			return
		}
		setRelayHeaders(w, radio.CodecContentType(st.Codec))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stream, err := s.radio.OpenRadioStream(ctx, userID, uuid)
	if err != nil {
		if errors.Is(err, service.ErrRadioBusy) {
			w.Header().Set("Retry-After", "5")
		}
		handleError(w, err)
		return
	}
	defer stream.Close()
	setRelayHeaders(w, stream.ContentType)
	w.Header().Set("Cache-Control", "no-store")
	// Players must not try to seek in a live stream.
	w.Header().Set("Accept-Ranges", "none")
	w.WriteHeader(http.StatusOK)
	relayStream(ctx, cancel, w, stream.Body)
}

// relayStream copies a live stream to the client until either side ends.
// The server's write timeout would cut every stream after 30 s, so each
// write gets its own deadline instead; a station that stops sending for
// radioIdleTimeout ends the relay.
func relayStream(ctx context.Context, cancel context.CancelFunc, w http.ResponseWriter, body io.Reader) {
	rc := http.NewResponseController(w)
	idle := time.AfterFunc(radioIdleTimeout, cancel)
	defer idle.Stop()
	buf := make([]byte, 16<<10)
	for ctx.Err() == nil {
		n, err := body.Read(buf)
		if n > 0 {
			idle.Reset(radioIdleTimeout)
			_ = rc.SetWriteDeadline(time.Now().Add(radioIdleTimeout))
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			_ = rc.Flush()
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) handleRadioFavicon(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.radio) {
		return
	}
	icon, err := s.radio.OpenRadioFavicon(r.Context(), userIDFromContext(r), r.PathValue("uuid"))
	if err != nil {
		handleError(w, err)
		return
	}
	defer icon.Close()
	data, err := io.ReadAll(io.LimitReader(icon.Body, radio.MaxFaviconBytes+1))
	if err != nil || len(data) > radio.MaxFaviconBytes {
		notFound(w)
		return
	}
	setRelayHeaders(w, icon.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
