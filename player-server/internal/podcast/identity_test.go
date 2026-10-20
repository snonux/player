package podcast

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

// identityVectors mirrors testdata/identity-vectors.json, the cross-client
// fixture of the podcast synchronization contract. The Android client must
// pass the same file (see docs/podcast-sync-contract.md).
type identityVectors struct {
	Version  int `json:"version"`
	FeedKeys []struct {
		Input string `json:"input"`
		Key   string `json:"key"`
	} `json:"feed_keys"`
	InvalidFeedURLs  []string `json:"invalid_feed_urls"`
	EpisodeDocuments []struct {
		Name  string         `json:"name"`
		Items []IdentityItem `json:"items"`
		Keys  []string       `json:"keys"`
	} `json:"episode_documents"`
	IngestScenarios []struct {
		Name    string         `json:"name"`
		Known   []KnownEpisode `json:"known"`
		Items   []IdentityItem `json:"items"`
		Results []IngestMatch  `json:"results"`
	} `json:"ingest_scenarios"`
	ClientResolution []struct {
		Name         string         `json:"name"`
		Known        []KnownEpisode `json:"known"`
		Key          string         `json:"key"`
		EnclosureURL string         `json:"enclosure_url"`
		Resolved     string         `json:"resolved"`
		OK           bool           `json:"ok"`
	} `json:"client_resolution"`
}

func loadIdentityVectors(t *testing.T) identityVectors {
	t.Helper()
	data, err := os.ReadFile("testdata/identity-vectors.json")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var v identityVectors
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	if v.Version != 3 || len(v.FeedKeys) == 0 || len(v.InvalidFeedURLs) == 0 || len(v.EpisodeDocuments) == 0 || len(v.IngestScenarios) == 0 || len(v.ClientResolution) == 0 {
		t.Fatalf("vectors incomplete: %+v", v)
	}
	return v
}

func TestFeedKeyVectors(t *testing.T) {
	for _, tc := range loadIdentityVectors(t).FeedKeys {
		got, err := FeedKey(tc.Input)
		if err != nil || got != tc.Key {
			t.Errorf("FeedKey(%q) = %q, %v; want %q", tc.Input, got, err, tc.Key)
		}
	}
}

func TestFeedKeyRejectsInvalidVectors(t *testing.T) {
	for _, input := range loadIdentityVectors(t).InvalidFeedURLs {
		got, err := FeedKey(input)
		if !errors.Is(err, ErrInvalidIdentityURL) || got != "" {
			t.Errorf("FeedKey(%q) = %q, %v; want ErrInvalidIdentityURL", input, got, err)
		}
	}
}

func TestEpisodeKeyVectors(t *testing.T) {
	for _, doc := range loadIdentityVectors(t).EpisodeDocuments {
		got := EpisodeKeys(doc.Items)
		if strings.Join(got, "\n") != strings.Join(doc.Keys, "\n") || len(got) != len(doc.Keys) {
			t.Errorf("%s: EpisodeKeys = %q; want %q", doc.Name, got, doc.Keys)
		}
	}
}

func TestMatchEpisodesVectors(t *testing.T) {
	for _, sc := range loadIdentityVectors(t).IngestScenarios {
		got := MatchEpisodes(sc.Known, sc.Items)
		if len(got) != len(sc.Results) {
			t.Errorf("%s: %d results, want %d", sc.Name, len(got), len(sc.Results))
			continue
		}
		for i := range got {
			if got[i] != sc.Results[i] {
				t.Errorf("%s: item %d = %+v, want %+v", sc.Name, i, got[i], sc.Results[i])
			}
		}
	}
}

// TestMatchEpisodesClaimsEachEpisodeOnce feeds every scenario's items twice
// in one document and checks that no known episode is matched twice and no
// key is created twice.
func TestMatchEpisodesClaimsEachEpisodeOnce(t *testing.T) {
	for _, sc := range loadIdentityVectors(t).IngestScenarios {
		items := append(append([]IdentityItem{}, sc.Items...), sc.Items...)
		seen := map[string]bool{}
		for _, r := range MatchEpisodes(sc.Known, items) {
			if r.Key != "" && seen[r.Key] {
				t.Errorf("%s: key %q assigned twice", sc.Name, r.Key)
			}
			seen[r.Key] = true
		}
	}
}

func TestResolveClientEpisodeVectors(t *testing.T) {
	for _, tc := range loadIdentityVectors(t).ClientResolution {
		got, ok := ResolveClientEpisode(tc.Known, tc.Key, tc.EnclosureURL)
		if got != tc.Resolved || ok != tc.OK {
			t.Errorf("%s: ResolveClientEpisode = %q, %v; want %q, %v", tc.Name, got, ok, tc.Resolved, tc.OK)
		}
	}
}

// TestEpisodeKeysOrderIndependent checks, for every vector document, that
// reversing the items yields the same keys: two replicas that saw the same
// items in different orders must mint the same identities. Fallback items
// sharing one enclosure share one key, so only which of them keeps it may
// differ.
func TestEpisodeKeysOrderIndependent(t *testing.T) {
	for _, doc := range loadIdentityVectors(t).EpisodeDocuments {
		n := len(doc.Items)
		reversed := make([]IdentityItem, n)
		for i, item := range doc.Items {
			reversed[n-1-i] = item
		}
		forward, backward := EpisodeKeys(doc.Items), EpisodeKeys(reversed)
		for i := range forward {
			f, b := forward[i], backward[n-1-i]
			if f != b && (f != "" && b != "" || strings.HasPrefix(f+b, guidKeyPrefix)) {
				t.Errorf("%s: item %d keyed %q forward, %q reversed", doc.Name, i, f, b)
			}
		}
		if !sameKeySet(forward, backward) {
			t.Errorf("%s: key sets differ: %q vs %q", doc.Name, forward, backward)
		}
	}
}

func sameKeySet(a, b []string) bool {
	set := map[string]int{}
	for _, k := range a {
		if k != "" {
			set[k]++
		}
	}
	for _, k := range b {
		if k != "" {
			set[k]--
		}
	}
	for _, n := range set {
		if n != 0 {
			return false
		}
	}
	return true
}

func TestMatchEpisodesCapsNewEpisodes(t *testing.T) {
	items := make([]IdentityItem, MaxNewEpisodesPerIngest+5)
	for i := range items {
		items[i] = IdentityItem{GUID: "g" + strconv.Itoa(i), EnclosureURL: "https://cdn.example.com/" + strconv.Itoa(i) + ".mp3"}
	}
	created := 0
	for i, r := range MatchEpisodes(nil, items) {
		if r.New {
			created++
		} else if i < MaxNewEpisodesPerIngest || r.Key != "" {
			t.Fatalf("item %d: unexpected result %+v", i, r)
		}
	}
	if created != MaxNewEpisodesPerIngest {
		t.Errorf("created %d episodes, want %d", created, MaxNewEpisodesPerIngest)
	}
}

func TestIdentityRejectsInvalidUTF8(t *testing.T) {
	if _, err := FeedKey("https://example.com/\xff"); !errors.Is(err, ErrInvalidIdentityURL) {
		t.Errorf("FeedKey with invalid UTF-8: err = %v", err)
	}
	keys := EpisodeKeys([]IdentityItem{{GUID: "\xff", EnclosureURL: "https://cdn.example.com/1.mp3"}})
	if keys[0] != "enc:cdn.example.com/1.mp3" {
		t.Errorf("invalid UTF-8 GUID: key = %q, want enclosure fallback", keys[0])
	}
}

func TestIdentityLengthLimits(t *testing.T) {
	tests := []struct {
		name string
		got  func() (string, error)
		want string
		err  bool
	}{
		{"url at limit", func() (string, error) {
			return FeedKey("https://e.com/" + strings.Repeat("a", MaxIdentityURLBytes-len("https://e.com/")))
		}, "feed:e.com/" + strings.Repeat("a", MaxIdentityURLBytes-len("https://e.com/")), false},
		{"url over limit", func() (string, error) {
			return FeedKey("https://e.com/" + strings.Repeat("a", MaxIdentityURLBytes))
		}, "", true},
		{"guid at limit stays verbatim", func() (string, error) {
			return GUIDKey(strings.Repeat("g", maxVerbatimGUIDBytes)), nil
		}, "guid:" + strings.Repeat("g", maxVerbatimGUIDBytes), false},
		{"overlong guid is hashed", func() (string, error) {
			return GUIDKey(strings.Repeat("g", maxVerbatimGUIDBytes+1)), nil
		}, "guid:sha256:66fe0c847a29ef060ea49fef22140a713e51c0df34229ae3954baa4d9782cb7b", false},
	}
	for _, tc := range tests {
		got, err := tc.got()
		if (err != nil) != tc.err {
			t.Errorf("%s: err = %v, want error %v", tc.name, err, tc.err)
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
