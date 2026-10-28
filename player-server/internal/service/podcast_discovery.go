package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrDiscoveryQuery = errors.New("enter 2 to 100 characters to search podcasts")
var ErrDiscoveryUnavailable = errors.New("podcast directory unavailable; retry shortly or add a feed URL")
var ErrDiscoveryBusy = errors.New("podcast directory is busy; retry in a few seconds")

type PodcastDiscoveryResult struct {
	Title   string `json:"title"`
	Author  string `json:"author"`
	FeedURL string `json:"feed_url"`
}

type discoveryEntry struct {
	results []PodcastDiscoveryResult
	expires time.Time
}

// PodcastDiscovery sends only explicit queries to a fixed public directory.
// It never fetches result URLs or artwork. Cache/rate limits are per server.
type PodcastDiscovery struct {
	client *http.Client
	mu     sync.Mutex
	cache  map[string]discoveryEntry
	next   time.Time
}

func NewPodcastDiscovery() *PodcastDiscovery {
	client := newRestrictedClient(15*time.Second, publicUnicast, 2<<20)
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 || r.URL.Scheme != "https" || r.URL.Host != "itunes.apple.com" {
			return ErrDiscoveryUnavailable
		}
		return nil
	}
	return &PodcastDiscovery{client: client, cache: make(map[string]discoveryEntry)}
}
func (d *PodcastDiscovery) Search(ctx context.Context, query string) ([]PodcastDiscoveryResult, error) {
	query = strings.TrimSpace(query)
	if n := utf8.RuneCountInString(query); n < 2 || n > 100 {
		return nil, ErrDiscoveryQuery
	}
	return d.discover(ctx, query, "US", false)
}

func (d *PodcastDiscovery) Popular(ctx context.Context, country string) ([]PodcastDiscoveryResult, error) {
	country = strings.ToUpper(country)
	if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
		return nil, ErrDiscoveryQuery
	}
	return d.discover(ctx, "", country, true)
}
func (d *PodcastDiscovery) discover(ctx context.Context, query, country string, popular bool) ([]PodcastDiscoveryResult, error) {
	key := country + ":" + strings.ToLower(query)
	now := time.Now()
	d.mu.Lock()
	if entry, ok := d.cache[key]; ok && now.Before(entry.expires) {
		d.mu.Unlock()
		return entry.results, nil
	}
	if now.Before(d.next) {
		d.mu.Unlock()
		return nil, ErrDiscoveryBusy
	}
	d.next = now.Add(3 * time.Second)
	if popular {
		d.next = now.Add(6 * time.Second)
	}
	d.mu.Unlock()
	u := "https://itunes.apple.com/search?" + url.Values{"term": {query}, "media": {"podcast"}, "entity": {"podcast"}, "limit": {"30"}, "country": {country}}.Encode()
	if popular {
		raw, err := d.get(ctx, "https://itunes.apple.com/"+strings.ToLower(country)+"/rss/toppodcasts/limit=20/json")
		if err != nil {
			return nil, err
		}
		var chart struct {
			Feed struct {
				Entries []struct {
					ID struct {
						Attributes struct {
							ID string `json:"im:id"`
						} `json:"attributes"`
					} `json:"id"`
				} `json:"entry"`
			} `json:"feed"`
		}
		if json.Unmarshal(raw, &chart) != nil {
			return nil, ErrDiscoveryUnavailable
		}
		ids := []string{}
		for _, entry := range chart.Feed.Entries {
			if _, err := strconv.ParseUint(entry.ID.Attributes.ID, 10, 64); err == nil {
				ids = append(ids, entry.ID.Attributes.ID)
			}
			if len(ids) == 20 {
				break
			}
		}
		if len(ids) == 0 {
			return []PodcastDiscoveryResult{}, nil
		}
		u = "https://itunes.apple.com/lookup?" + url.Values{"id": {strings.Join(ids, ",")}, "entity": {"podcast"}, "country": {country}}.Encode()
	}
	raw, err := d.get(ctx, u)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Results []struct {
			Title  string `json:"collectionName"`
			Author string `json:"artistName"`
			URL    string `json:"feedUrl"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil, ErrDiscoveryUnavailable
	}
	results := make([]PodcastDiscoveryResult, 0, 30)
	seen := make(map[string]bool)
	for _, item := range payload.Results {
		u, err := url.Parse(item.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || item.Title == "" || seen[item.URL] {
			continue
		}
		seen[item.URL] = true
		results = append(results, PodcastDiscoveryResult{item.Title, item.Author, item.URL})
		if len(results) == 30 {
			break
		}
	}
	d.mu.Lock()
	if len(d.cache) >= 32 {
		clear(d.cache)
	}
	d.cache[key] = discoveryEntry{results, now.Add(15 * time.Minute)}
	d.mu.Unlock()
	return results, nil
}

func (d *PodcastDiscovery) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, ErrDiscoveryUnavailable
	}
	req.Header.Set("Accept", "application/json")
	response, err := d.client.Do(req)
	if err != nil {
		return nil, ErrDiscoveryUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrDiscoveryUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return nil, ErrDiscoveryUnavailable
	}
	return raw, nil
}
