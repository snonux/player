package podcast

import "strings"

// MaxNewEpisodesPerIngest caps the episodes one ingest may create for a
// feed. Items beyond it are left for the next fetch, so a hostile or broken
// feed cannot flood the catalog in one request.
const MaxNewEpisodesPerIngest = 1000

// KnownEpisode is an episode already in a catalog (server or device), as
// episode ingest sees it.
type KnownEpisode struct {
	// Key is the episode's canonical key; it never changes.
	Key string `json:"key"`
	// Aliases are other keys recorded for the episode (at most
	// MaxEpisodeAliases).
	Aliases []string `json:"aliases"`
	// EnclosureKey is the enclosure key of the latest document that carried
	// the episode. It is overwritten on every ingest and is not an alias, so
	// rotating (signed) enclosure URLs do not accumulate.
	EnclosureKey string `json:"enclosure_key"`
}

// IngestMatch is the ingest outcome for one item of a feed document.
type IngestMatch struct {
	// Key is the canonical key of the item's episode, "" when the item is
	// not an episode, a duplicate of another item, or over the new-episode cap.
	Key string `json:"key"`
	// New reports that an episode with Key must be created.
	New bool `json:"new"`
	// Alias is a key to record as alias of Key, or "".
	Alias string `json:"alias"`
}

// episodeIndex looks up the known episodes of one feed by key, alias and
// enclosure.
type episodeIndex struct {
	known []KnownEpisode
	byKey map[string]int   // canonical key or alias → known index (-1: new)
	byEnc map[string][]int // enc: key, enc: alias or enclosure_key → known indexes
}

// episodeMatcher holds the state of one MatchEpisodes run.
type episodeMatcher struct {
	episodeIndex
	keys    []string // EpisodeKeys of the document
	encs    []string // enclosure key per item, "" when unusable
	aliases []int    // alias count per known episode
	claimed []bool   // known episode already matched by an earlier item
	docKeys map[string]bool
	results []IngestMatch
}

func newEpisodeIndex(known []KnownEpisode) episodeIndex {
	ix := episodeIndex{known: known, byKey: map[string]int{}, byEnc: map[string][]int{}}
	for i, ep := range known {
		ix.byKey[ep.Key] = i
		ix.addEnc(ep.Key, i)
		ix.addEnc(ep.EnclosureKey, i)
		for _, alias := range ep.Aliases {
			ix.byKey[alias] = i
			ix.addEnc(alias, i)
		}
	}
	return ix
}

// addEnc indexes an enclosure key (or enclosure alias) of known episode i.
func (ix *episodeIndex) addEnc(key string, i int) {
	if !strings.HasPrefix(key, enclosureKeyPrefix) {
		return
	}
	for _, j := range ix.byEnc[key] {
		if j == i {
			return
		}
	}
	ix.byEnc[key] = append(ix.byEnc[key], i)
}

// encMatch returns the single known episode carrying enclosure key enc, or -1.
func (ix *episodeIndex) encMatch(enc string) int {
	if matches := ix.byEnc[enc]; enc != "" && len(matches) == 1 {
		return matches[0]
	}
	return -1
}

// preferEnclosure applies the rule shared by ingest and client resolution:
// a key match x yields to y, the single episode carrying the enclosure, when
// y is a different episode identified by an enc: key and x does not carry
// that enclosure itself. Such a y exists because the GUID was ambiguous (it
// repeated in a document), so the GUID is not trusted over the enclosure.
func (ix *episodeIndex) preferEnclosure(x int, enc string) int {
	y := ix.encMatch(enc)
	if y >= 0 && y != x && strings.HasPrefix(ix.known[y].Key, enclosureKeyPrefix) &&
		(x < 0 || ix.known[x].EnclosureKey != enc) {
		return y
	}
	return x
}

// ResolveClientEpisode maps an episode key a client sent for one feed, with
// the enclosure URL it saw as evidence, to the canonical key of a known
// episode (contract §3.3). ok is false when the key must be kept as a
// state-only key of that user until an ingest learns it. Client evidence
// never links two GUIDs: an unknown guid: key is never resolved through its
// enclosure, since only server ingest may record a GUID change as an alias.
func ResolveClientEpisode(known []KnownEpisode, key, enclosureURL string) (string, bool) {
	ix := newEpisodeIndex(known)
	enc, err := EnclosureKey(enclosureURL)
	if err != nil {
		enc = ""
	}
	x, found := ix.byKey[key]
	if !found {
		x = -1
	}
	if x < 0 && strings.HasPrefix(key, enclosureKeyPrefix) {
		if x = ix.encMatch(key); x < 0 {
			x = ix.encMatch(enc)
		}
	}
	if x < 0 {
		return "", false
	}
	if enc != "" {
		x = ix.preferEnclosure(x, enc)
	}
	return ix.known[x].Key, true
}

// MatchEpisodes resolves every item of a freshly fetched feed document
// against the episodes already known for that feed (contract §3.3). It is a
// pure function shared by the server and, through the identity vectors, by
// the Android client. Every known episode is matched by at most one item:
//
//  1. Key pass: an item whose key is a known key or alias claims that
//     episode, unless preferEnclosure points at another episode.
//  2. Enclosure pass: an unmatched item claims the one episode carrying its
//     enclosure, if that episode is unclaimed and its own key is not the key
//     of any item in the document (its GUID changed, vanished or started
//     repeating).
//  3. New pass: remaining items become new episodes under their key, or
//     their enclosure key when the key already belongs to an episode; an
//     item for which both are taken is a duplicate and skipped. At most
//     MaxNewEpisodesPerIngest new episodes are created, in document order.
//
// The caller stores each matched or new episode's EnclosureKey from the item.
func MatchEpisodes(known []KnownEpisode, items []IdentityItem) []IngestMatch {
	m := newEpisodeMatcher(known, items)
	m.matchByKey()
	m.matchByEnclosure()
	m.addNew()
	return m.results
}

func newEpisodeMatcher(known []KnownEpisode, items []IdentityItem) *episodeMatcher {
	m := &episodeMatcher{
		episodeIndex: newEpisodeIndex(known), keys: EpisodeKeys(items),
		encs: make([]string, len(items)), aliases: make([]int, len(known)),
		claimed: make([]bool, len(known)), docKeys: map[string]bool{},
		results: make([]IngestMatch, len(items)),
	}
	for i, ep := range known {
		m.aliases[i] = len(ep.Aliases)
	}
	for i, item := range items {
		if m.keys[i] == "" {
			continue
		}
		m.encs[i], _ = EnclosureKey(item.EnclosureURL)
		m.docKeys[m.keys[i]] = true
	}
	return m
}

func (m *episodeMatcher) matchByKey() {
	for i, key := range m.keys {
		if key == "" {
			continue
		}
		x, found := m.byKey[key]
		if !found {
			x = -1
		}
		if x = m.preferEnclosure(x, m.encs[i]); x >= 0 && !m.claimed[x] {
			m.claim(i, x)
		}
	}
}

func (m *episodeMatcher) matchByEnclosure() {
	for i, key := range m.keys {
		if key == "" || m.results[i].Key != "" {
			continue
		}
		x := m.encMatch(m.encs[i])
		if x >= 0 && !m.claimed[x] && !m.docKeys[m.known[x].Key] {
			m.claim(i, x)
		}
	}
}

func (m *episodeMatcher) addNew() {
	created := 0
	for i, key := range m.keys {
		if key == "" || m.results[i].Key != "" || created >= MaxNewEpisodesPerIngest {
			continue
		}
		if _, taken := m.byKey[key]; taken {
			key = m.encs[i]
		}
		if _, taken := m.byKey[key]; taken {
			continue // duplicate of an episode another item already holds
		}
		m.byKey[key] = -1
		m.results[i] = IngestMatch{Key: key, New: true}
		created++
	}
}

// claim matches item i to known episode x and records the item's key as an
// alias when it is new to the feed and under the alias cap.
func (m *episodeMatcher) claim(i, x int) {
	m.claimed[x] = true
	m.results[i] = IngestMatch{Key: m.known[x].Key}
	key := m.keys[i]
	if _, known := m.byKey[key]; known || m.aliases[x] >= MaxEpisodeAliases {
		return
	}
	m.byKey[key] = x
	m.aliases[x]++
	m.results[i].Alias = key
}
