package podcast

import (
	"encoding/json"
	"errors"
	"os"
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
	if v.Version != 1 || len(v.FeedKeys) == 0 || len(v.InvalidFeedURLs) == 0 || len(v.EpisodeDocuments) == 0 {
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
