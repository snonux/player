package podcast

import "strings"

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
	// not an episode or a duplicate of another item.
	Key string `json:"key"`
	// New reports that an episode with Key must be created.
	New bool `json:"new"`
	// Alias is a key to record as alias of Key, or "".
	Alias string `json:"alias"`
}

// episodeMatcher holds the state of one MatchEpisodes run.
type episodeMatcher struct {
	known    []KnownEpisode
	keys     []string // EpisodeKeys of the document
	encs     []string // enclosure key per item, "" when unusable
	reliable []bool   // item key identifies the item (not a repeated GUID)
	byKey    map[string]int
	byEnc    map[string][]int
	aliases  []int  // alias count per known episode
	claimed  []bool // known episode already matched by an earlier item
	docKeys  map[string]bool
	results  []IngestMatch
}

// MatchEpisodes resolves every item of a freshly fetched feed document
// against the episodes already known for that feed (contract §3.3). It is a
// pure function shared by the server and, through the identity vectors, by
// the Android client. Every known episode is matched by at most one item:
//
//  1. Key pass: an item whose key is a known key or alias claims that
//     episode, unless its enclosure singles out a different episode whose
//     canonical key is an enclosure key (that episode exists because the
//     key was ambiguous). The key of the first item of a GUID repeated in
//     the document is ambiguous and not used here.
//  2. Enclosure pass: an unmatched item claims the one episode carrying its
//     enclosure, if that episode is unclaimed and its own key is not the
//     unambiguous key of any item in the document (its GUID changed,
//     vanished or became ambiguous).
//  3. New pass: remaining items become new episodes under their key, or
//     their enclosure key when the key already belongs to an episode; an
//     item for which both are taken is a duplicate and skipped.
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
		known: known, keys: EpisodeKeys(items), encs: make([]string, len(items)),
		reliable: make([]bool, len(items)), byKey: map[string]int{}, byEnc: map[string][]int{},
		aliases: make([]int, len(known)), claimed: make([]bool, len(known)),
		docKeys: map[string]bool{}, results: make([]IngestMatch, len(items)),
	}
	for i, ep := range known {
		m.byKey[ep.Key] = i
		m.addEnc(ep.Key, i)
		m.addEnc(ep.EnclosureKey, i)
		for _, alias := range ep.Aliases {
			m.byKey[alias] = i
			m.addEnc(alias, i)
		}
		m.aliases[i] = len(ep.Aliases)
	}
	m.markReliable(items)
	return m
}

// addEnc indexes an enclosure key (or enclosure alias) of known episode i.
func (m *episodeMatcher) addEnc(key string, i int) {
	if !strings.HasPrefix(key, enclosureKeyPrefix) {
		return
	}
	for _, j := range m.byEnc[key] {
		if j == i {
			return
		}
	}
	m.byEnc[key] = append(m.byEnc[key], i)
}

// markReliable records each item's enclosure key and whether its key is
// unambiguous: a GUID key is ambiguous when the GUID repeats in the document.
func (m *episodeMatcher) markReliable(items []IdentityItem) {
	guidCount := map[string]int{}
	for i, item := range items {
		if enc, err := EnclosureKey(item.EnclosureURL); err == nil {
			m.encs[i] = enc
			if guid := itemGUID(item); guid != "" {
				guidCount[guid]++
			}
		}
	}
	for i, item := range items {
		repeated := guidCount[itemGUID(item)] > 1 && strings.HasPrefix(m.keys[i], guidKeyPrefix)
		m.reliable[i] = m.keys[i] != "" && !repeated
		if m.reliable[i] {
			m.docKeys[m.keys[i]] = true
		}
	}
}

// encMatch returns the single known episode carrying enclosure key enc, or -1.
func (m *episodeMatcher) encMatch(enc string) int {
	if matches := m.byEnc[enc]; enc != "" && len(matches) == 1 {
		return matches[0]
	}
	return -1
}

func (m *episodeMatcher) matchByKey() {
	for i, key := range m.keys {
		if key == "" {
			continue
		}
		byKey := -1
		if x, ok := m.byKey[key]; ok && m.reliable[i] {
			byKey = x
		}
		byEnc := m.encMatch(m.encs[i])
		choice := byKey
		if byEnc >= 0 && byEnc != byKey && strings.HasPrefix(m.known[byEnc].Key, enclosureKeyPrefix) {
			choice = byEnc
		}
		if choice >= 0 && !m.claimed[choice] {
			m.claim(i, choice)
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
	for i, key := range m.keys {
		if key == "" || m.results[i].Key != "" {
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
	}
}

// claim matches item i to known episode x and records the item's key as an
// alias when it is unambiguous, new to the feed and under the alias cap.
func (m *episodeMatcher) claim(i, x int) {
	m.claimed[x] = true
	m.results[i] = IngestMatch{Key: m.known[x].Key}
	key := m.keys[i]
	if _, known := m.byKey[key]; known || !m.reliable[i] || m.aliases[x] >= MaxEpisodeAliases {
		return
	}
	m.byKey[key] = x
	m.aliases[x]++
	m.results[i].Alias = key
}
