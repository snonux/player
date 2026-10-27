// Package radio talks to the Radio Browser directory (radio-browser.info)
// and fetches station streams and favicons for the server to relay.
//
// Clients never contact Radio Browser or a station host themselves: the
// server searches the directory and relays the audio, so the browser sees no
// mixed content, Android needs no cleartext exception, and every request
// goes through the normal session check.
package radio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/snonux/player/internal/model"
)

// maxDirectoryBody bounds one directory answer. A full country list is
// about 30 KB and a page of 100 stations about 150 KB.
const maxDirectoryBody = 4 << 20

// ErrUnavailable wraps every failure to get a usable answer from the
// directory.
var ErrUnavailable = errors.New("radio directory unavailable")

// ErrStationNotFound is returned when the directory does not know a station.
var ErrStationNotFound = errors.New("radio station not found")

// Directory looks up stations, tags and countries.
type Directory interface {
	Search(ctx context.Context, q model.RadioQuery) ([]model.RadioStation, error)
	Station(ctx context.Context, uuid string) (*model.RadioStation, error)
	Tags(ctx context.Context, limit int) ([]model.RadioTag, error)
	Countries(ctx context.Context) ([]model.RadioCountry, error)
	// CountClick tells the directory a station was played, which feeds its
	// popularity ranking. Failures are not interesting to the caller.
	CountClick(ctx context.Context, uuid string)
}

// BrowserDirectory is the Radio Browser implementation of Directory.
type BrowserDirectory struct {
	base      string
	client    *http.Client
	userAgent string
}

// NewBrowserDirectory returns a directory client for the API at baseURL.
// Radio Browser asks for a descriptive User-Agent.
func NewBrowserDirectory(baseURL string, client *http.Client, userAgent string) *BrowserDirectory {
	return &BrowserDirectory{base: strings.TrimRight(baseURL, "/"), client: client, userAgent: userAgent}
}

// browserStation is one station as Radio Browser returns it.
type browserStation struct {
	UUID        string `json:"stationuuid"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	URLResolved string `json:"url_resolved"`
	Homepage    string `json:"homepage"`
	Favicon     string `json:"favicon"`
	Tags        string `json:"tags"`
	Country     string `json:"country"`
	CountryCode string `json:"countrycode"`
	Language    string `json:"language"`
	Codec       string `json:"codec"`
	Bitrate     int    `json:"bitrate"`
	Votes       int    `json:"votes"`
	HLS         int    `json:"hls"`
}

// station converts a directory entry. ok is false for entries that cannot
// be relayed: HLS (a playlist of segments, not one stream) and entries
// without a usable http(s) stream URL.
func (b browserStation) station() (model.RadioStation, bool) {
	stream := strings.TrimSpace(b.URLResolved)
	if stream == "" {
		stream = strings.TrimSpace(b.URL)
	}
	if b.HLS != 0 || !model.ValidRadioUUID(b.UUID) || !httpURL(stream) {
		return model.RadioStation{}, false
	}
	favicon := strings.TrimSpace(b.Favicon)
	if !httpURL(favicon) {
		favicon = ""
	}
	homepage := strings.TrimSpace(b.Homepage)
	if !httpURL(homepage) {
		homepage = ""
	}
	return model.RadioStation{
		UUID:        b.UUID,
		Name:        strings.TrimSpace(b.Name),
		StreamURL:   stream,
		Homepage:    homepage,
		FaviconURL:  favicon,
		Country:     b.Country,
		CountryCode: strings.ToUpper(b.CountryCode),
		Language:    b.Language,
		Tags:        model.SplitRadioTags(b.Tags),
		Codec:       b.Codec,
		Bitrate:     b.Bitrate,
		Votes:       b.Votes,
	}, true
}

func httpURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// orderParams maps the API's order names to Radio Browser's.
var orderParams = map[string][2]string{
	model.RadioOrderPopular: {"clickcount", "true"},
	model.RadioOrderVotes:   {"votes", "true"},
	model.RadioOrderName:    {"name", "false"},
	model.RadioOrderRandom:  {"random", "false"},
}

// Search runs a station search. HLS stations are dropped from the page, so
// a page can hold fewer stations than requested.
func (d *BrowserDirectory) Search(ctx context.Context, q model.RadioQuery) ([]model.RadioStation, error) {
	params := url.Values{}
	params.Set("hidebroken", "true")
	params.Set("limit", strconv.Itoa(q.Limit))
	params.Set("offset", strconv.Itoa(q.Offset))
	order, ok := orderParams[q.Order]
	if !ok {
		order = orderParams[model.RadioOrderPopular]
	}
	params.Set("order", order[0])
	params.Set("reverse", order[1])
	if q.Name != "" {
		params.Set("name", q.Name)
	}
	if q.Tag != "" {
		params.Set("tag", q.Tag)
		params.Set("tagExact", "true")
	}
	if q.CountryCode != "" {
		params.Set("countrycode", q.CountryCode)
	}
	var raw []browserStation
	if err := d.get(ctx, "/json/stations/search", params, &raw); err != nil {
		return nil, err
	}
	return convertStations(raw), nil
}

// Station looks up one station by its UUID.
func (d *BrowserDirectory) Station(ctx context.Context, uuid string) (*model.RadioStation, error) {
	var raw []browserStation
	if err := d.get(ctx, "/json/stations/byuuid/"+url.PathEscape(uuid), nil, &raw); err != nil {
		return nil, err
	}
	stations := convertStations(raw)
	for i := range stations {
		if stations[i].UUID == uuid {
			return &stations[i], nil
		}
	}
	return nil, ErrStationNotFound
}

func convertStations(raw []browserStation) []model.RadioStation {
	stations := make([]model.RadioStation, 0, len(raw))
	for _, b := range raw {
		if s, ok := b.station(); ok {
			stations = append(stations, s)
		}
	}
	return stations
}

// Tags returns the tags with the most stations.
func (d *BrowserDirectory) Tags(ctx context.Context, limit int) ([]model.RadioTag, error) {
	params := url.Values{}
	params.Set("order", "stationcount")
	params.Set("reverse", "true")
	params.Set("hidebroken", "true")
	params.Set("limit", strconv.Itoa(limit))
	var raw []struct {
		Name         string `json:"name"`
		StationCount int    `json:"stationcount"`
	}
	if err := d.get(ctx, "/json/tags", params, &raw); err != nil {
		return nil, err
	}
	tags := make([]model.RadioTag, 0, len(raw))
	for _, t := range raw {
		if name := strings.TrimSpace(t.Name); name != "" {
			tags = append(tags, model.RadioTag{Name: name, StationCount: t.StationCount})
		}
	}
	return tags, nil
}

// Countries returns the countries that have stations, most stations first.
// Radio Browser can list one code under several spellings of the name; they
// are merged under the first (largest) one.
func (d *BrowserDirectory) Countries(ctx context.Context) ([]model.RadioCountry, error) {
	params := url.Values{}
	params.Set("order", "stationcount")
	params.Set("reverse", "true")
	params.Set("hidebroken", "true")
	var raw []struct {
		Name         string `json:"name"`
		Code         string `json:"iso_3166_1"`
		StationCount int    `json:"stationcount"`
	}
	if err := d.get(ctx, "/json/countries", params, &raw); err != nil {
		return nil, err
	}
	countries := make([]model.RadioCountry, 0, len(raw))
	index := map[string]int{}
	for _, c := range raw {
		code := strings.ToUpper(strings.TrimSpace(c.Code))
		if !model.ValidRadioCountryCode(code) || strings.TrimSpace(c.Name) == "" {
			continue
		}
		if i, ok := index[code]; ok {
			countries[i].StationCount += c.StationCount
			continue
		}
		index[code] = len(countries)
		countries = append(countries, model.RadioCountry{Code: code, Name: strings.TrimSpace(c.Name), StationCount: c.StationCount})
	}
	return countries, nil
}

// CountClick registers a play of the station with the directory.
func (d *BrowserDirectory) CountClick(ctx context.Context, uuid string) {
	var ignored json.RawMessage
	_ = d.get(ctx, "/json/url/"+url.PathEscape(uuid), nil, &ignored)
}

func (d *BrowserDirectory) get(ctx context.Context, path string, params url.Values, dst any) error {
	target := d.base + path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("User-Agent", d.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s answered %d", ErrUnavailable, path, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDirectoryBody+1))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if len(body) > maxDirectoryBody {
		return fmt.Errorf("%w: %s answer too large", ErrUnavailable, path)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnavailable, path, err)
	}
	return nil
}
