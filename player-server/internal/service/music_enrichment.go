package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

// EnrichmentIdentity sends existing tags, rather than guessed external identities.
type EnrichmentIdentity struct {
	Kind        string `json:"kind"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Artist      string `json:"artist,omitempty"`
	ReleaseMBID string `json:"release_mbid,omitempty"`
}

// EnrichmentResult describes data availability independently of primary details.
type EnrichmentResult struct {
	*model.MusicEnrichment
	Enabled    bool   `json:"enabled"`
	CanRefresh bool   `json:"can_refresh"`
	State      string `json:"state"`
	Stale      bool   `json:"stale"`
	Message    string `json:"message,omitempty"`
}

// EnrichmentProvider supplies optional descriptive metadata for a tagged identity.
type EnrichmentProvider interface {
	Fetch(context.Context, EnrichmentIdentity) (model.MusicEnrichment, error)
}

// HTTPEnrichmentProvider calls an operator-selected JSON agent, without sending credentials.
type HTTPEnrichmentProvider struct {
	endpoint string
	client   *http.Client
}

// NewHTTPEnrichmentProvider creates an adapter with a bounded request lifetime.
func NewHTTPEnrichmentProvider(endpoint string) *HTTPEnrichmentProvider {
	return &HTTPEnrichmentProvider{endpoint: endpoint, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Fetch accepts bounded plain text and HTTP(S) links. A 204 means no match.
func (p *HTTPEnrichmentProvider) Fetch(ctx context.Context, id EnrichmentIdentity) (model.MusicEnrichment, error) {
	result := model.MusicEnrichment{}
	u, err := url.Parse(p.endpoint)
	if err != nil || !safeMetadataURL(p.endpoint) {
		return result, fmt.Errorf("invalid enrichment agent URL")
	}
	q := u.Query()
	q.Set("kind", id.Kind)
	q.Set("key", id.Key)
	q.Set("name", id.Name)
	if id.Artist != "" {
		q.Set("artist", id.Artist)
	}
	if id.ReleaseMBID != "" {
		q.Set("release_mbid", id.ReleaseMBID)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("Accept", "application/json")
	response, err := p.client.Do(req)
	if err != nil {
		return result, fmt.Errorf("fetch metadata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return result, nil
	}
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("metadata agent returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 128*1024+1))
	if err != nil {
		return result, err
	}
	if len(data) > 128*1024 {
		return result, fmt.Errorf("metadata response too large")
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("decode metadata: %w", err)
	}
	if err := validateEnrichment(result); err != nil {
		return model.MusicEnrichment{}, err
	}
	return result, nil
}

// MusicEnrichmentIdentity resolves only already authorized catalog identities.
func MusicEnrichmentIdentity(albums []Album, kind, key string) (EnrichmentIdentity, bool) {
	if kind == "album" {
		for _, album := range albums {
			if album.Key == key {
				id := EnrichmentIdentity{Kind: kind, Key: key, Name: album.Title, Artist: album.Artist}
				for _, song := range album.Songs {
					if song.Music != nil && releaseIDPattern.MatchString(song.Music.ReleaseMBID) {
						id.ReleaseMBID = song.Music.ReleaseMBID
						break
					}
				}
				return id, true
			}
		}
	}
	if kind == "artist" {
		for _, artist := range MusicArtists(albums) {
			if artist.Key == key {
				return EnrichmentIdentity{Kind: kind, Key: key, Name: artist.Name}, true
			}
		}
	}
	return EnrichmentIdentity{}, false
}

// LoadMusicEnrichment retains the last good value on provider failures. Reads
// never perform external I/O: explicit admin refresh is the only fetch trigger.
func LoadMusicEnrichment(ctx context.Context, store repository.MusicEnrichmentRepo, provider EnrichmentProvider, namespace string, id EnrichmentIdentity, refresh, admin bool, now time.Time) (EnrichmentResult, error) {
	result := EnrichmentResult{MusicEnrichment: &model.MusicEnrichment{}, Enabled: provider != nil, CanRefresh: admin && provider != nil, State: "disabled"}
	if provider == nil {
		return result, nil
	}
	namespace = fmt.Sprintf("%x", sha256.Sum256([]byte(namespace)))
	cached, err := store.GetMusicEnrichment(ctx, namespace, id.Kind, id.Key)
	if err != nil {
		return result, err
	}
	result.State = "unfetched"
	if cached != nil {
		result.MusicEnrichment = cached
		result.State = enrichmentState(*cached)
		result.Stale = cached.FetchedAt == nil || now.Sub(*cached.FetchedAt) > 30*24*time.Hour
	}
	if !refresh {
		return result, nil
	}
	fresh, err := provider.Fetch(ctx, id)
	if err != nil {
		result.State = "error"
		result.Stale = cached != nil
		result.Message = "Metadata refresh failed. Check the configured provider and try again. Previous metadata and local artwork are retained."
		return result, nil
	}
	// Even injected providers must obey the same safe, bounded response contract.
	if err := validateEnrichment(fresh); err != nil {
		result.State = "error"
		result.Stale = cached != nil
		result.Message = "The metadata provider returned invalid data. Previous metadata is retained."
		return result, nil
	}
	fresh.FetchedAt = &now
	if err := store.SaveMusicEnrichment(ctx, namespace, id.Kind, id.Key, fresh); err != nil {
		return result, err
	}
	result.MusicEnrichment = &fresh
	result.State = enrichmentState(fresh)
	result.Stale = false
	return result, nil
}

func enrichmentState(item model.MusicEnrichment) string {
	if item.Description == "" && item.ImageURL == "" && len(item.Links) == 0 {
		return "empty"
	}
	return "ready"
}
func safeMetadataURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && len(value) <= 4096
}
func validateEnrichment(item model.MusicEnrichment) error {
	if len(item.Description) > 64000 || len(item.Source) > 200 || len(item.Links) > 12 {
		return fmt.Errorf("metadata fields exceed limits")
	}
	if enrichmentState(item) == "ready" && strings.TrimSpace(item.Source) == "" {
		return fmt.Errorf("metadata source required")
	}
	if item.ImageURL != "" && !safeMetadataURL(item.ImageURL) {
		return fmt.Errorf("invalid metadata image URL")
	}
	for _, link := range item.Links {
		if strings.TrimSpace(link.Label) == "" || len(link.Label) > 200 || !safeMetadataURL(link.URL) {
			return fmt.Errorf("invalid metadata link")
		}
	}
	return nil
}
