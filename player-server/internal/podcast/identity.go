package podcast

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Identity key prefixes and limits from the podcast synchronization contract
// (docs/podcast-sync-contract.md, "3. Stable identities"). Every client derives
// the same keys from the same feed document, so these values are part of the
// wire contract: changing one is a breaking change for every client.
const (
	feedKeyPrefix      = "feed:"
	guidKeyPrefix      = "guid:"
	enclosureKeyPrefix = "enc:"
	hashedKeyMarker    = "sha256:"

	// MaxIdentityURLBytes bounds feed and enclosure URLs accepted as identity.
	MaxIdentityURLBytes = 2048
	// maxVerbatimGUIDBytes is the longest GUID kept verbatim in a key; longer
	// GUIDs are hashed so keys stay bounded. Bytes of UTF-8, not characters.
	maxVerbatimGUIDBytes = 1024
	// MaxEpisodeAliases caps the aliases ingest records for one episode, so
	// a publisher that keeps changing GUIDs cannot grow the table unbounded.
	MaxEpisodeAliases = 16
	// MaxFeedAliases caps the aliases (permanent redirects, new-feed-url)
	// the server records for one feed.
	MaxFeedAliases = 16
)

// ErrInvalidIdentityURL reports a feed or enclosure URL that cannot be turned
// into a stable identity (unsupported scheme, credentials, no host, ...).
var ErrInvalidIdentityURL = errors.New("invalid podcast identity url")

// pseudoSchemes are podcast subscription link schemes that stand for HTTP.
var pseudoSchemes = map[string]bool{"feed": true, "itpc": true, "pcast": true, "podcast": true}

// IdentityItem is the identity evidence of one feed item, in document order.
type IdentityItem struct {
	GUID         string `json:"guid"`
	EnclosureURL string `json:"enclosure_url"`
}

// FeedKey returns the canonical identity of a feed URL. The key ignores the
// difference between http and https, default ports, host case, a trailing
// host dot, the fragment and percent-escape case; path and query are kept
// verbatim otherwise. It never changes for a subscription once minted:
// redirects are recorded as aliases instead.
func FeedKey(raw string) (string, error) {
	loc, err := canonicalLocation(raw)
	if err != nil {
		return "", err
	}
	return feedKeyPrefix + loc, nil
}

// EnclosureKey returns the fallback episode identity derived from an
// enclosure URL, canonicalized like a feed URL.
func EnclosureKey(raw string) (string, error) {
	loc, err := canonicalLocation(raw)
	if err != nil {
		return "", err
	}
	return enclosureKeyPrefix + loc, nil
}

// GUIDKey returns the episode identity for a trimmed, non-empty GUID. GUIDs
// are compared case-sensitively; overlong ones are replaced by their hash.
func GUIDKey(guid string) string {
	if len(guid) > maxVerbatimGUIDBytes {
		sum := sha256.Sum256([]byte(guid))
		return guidKeyPrefix + hashedKeyMarker + hex.EncodeToString(sum[:])
	}
	return guidKeyPrefix + guid
}

// EpisodeKeys returns one episode key per item of a feed document, in the
// same order, or "" for an item that is not a playable, distinct episode.
// The result for an item does not depend on the order of the items, so two
// replicas that fetched the same items in different orders agree:
//   - an item without a valid enclosure URL is skipped;
//   - an item whose GUID occurs in no other item uses it ("guid:...");
//   - an item without a GUID, or whose GUID occurs in several items (none of
//     them then gets "guid:..."), falls back to its enclosure ("enc:..."),
//     unless an item with a unique GUID or an earlier fallback item has that
//     enclosure, in which case it is a duplicate and skipped.
func EpisodeKeys(items []IdentityItem) []string {
	keys := make([]string, len(items))
	encs := make([]string, len(items))
	guidCount := map[string]int{}
	for i, item := range items {
		if enc, err := EnclosureKey(item.EnclosureURL); err == nil {
			encs[i] = enc
			guidCount[itemGUID(item)]++
		}
	}
	taken := map[string]bool{}
	for i, item := range items {
		if guid := itemGUID(item); encs[i] != "" && guid != "" && guidCount[guid] == 1 {
			keys[i] = GUIDKey(guid)
			taken[encs[i]] = true
		}
	}
	for i, enc := range encs {
		if enc != "" && keys[i] == "" && !taken[enc] {
			keys[i] = enc
			taken[enc] = true
		}
	}
	return keys
}

// itemGUID returns the trimmed GUID of an item; a GUID that is not valid
// UTF-8 counts as missing.
func itemGUID(item IdentityItem) string {
	if !utf8.ValidString(item.GUID) {
		return ""
	}
	return trimASCIISpace(item.GUID)
}

// canonicalLocation turns an absolute http(s) URL into its scheme-less
// canonical form "host[:port]/path[?query]".
func canonicalLocation(raw string) (string, error) {
	s := trimASCIISpace(raw)
	if s == "" || len(s) > MaxIdentityURLBytes {
		return "", fmt.Errorf("%w: empty or longer than %d bytes", ErrInvalidIdentityURL, MaxIdentityURLBytes)
	}
	if !utf8.ValidString(s) {
		return "", fmt.Errorf("%w: not valid UTF-8", ErrInvalidIdentityURL)
	}
	for i := 0; i < len(s); i++ {
		if s[i] <= 0x20 || s[i] == 0x7f {
			return "", fmt.Errorf("%w: contains whitespace or control characters", ErrInvalidIdentityURL)
		}
	}
	scheme, rest, err := splitScheme(s, true)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(rest, "//") {
		return "", fmt.Errorf("%w: not an absolute url", ErrInvalidIdentityURL)
	}
	rest = rest[2:]
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	authority, err := canonicalAuthority(rest[:end], scheme)
	if err != nil {
		return "", err
	}
	return authority + canonicalPathQuery(rest[end:]), nil
}

// splitScheme returns the lower-case http or https scheme of s and the text
// after the colon. Podcast pseudo schemes map to http; "feed:https://..."
// and "feed://https://..." style wrappers are unwrapped once.
func splitScheme(s string, unwrap bool) (string, string, error) {
	colon := strings.IndexByte(s, ':')
	if colon <= 0 || !validScheme(s[:colon]) {
		return "", "", fmt.Errorf("%w: missing scheme", ErrInvalidIdentityURL)
	}
	scheme, rest := strings.ToLower(s[:colon]), s[colon+1:]
	if pseudoSchemes[scheme] && !unwrap {
		return "", "", fmt.Errorf("%w: nested podcast scheme", ErrInvalidIdentityURL)
	}
	if pseudoSchemes[scheme] {
		if inner := strings.TrimPrefix(rest, "//"); unwrap && wrapsURL(inner) {
			return splitScheme(inner, false)
		}
		scheme = "http"
	}
	if scheme != "http" && scheme != "https" {
		return "", "", fmt.Errorf("%w: unsupported scheme %q", ErrInvalidIdentityURL, scheme)
	}
	return scheme, rest, nil
}

// wrapsURL reports whether s starts with "<scheme>://", i.e. a pseudo
// scheme wraps a complete URL, whose own scheme then decides validity.
func wrapsURL(s string) bool {
	colon := strings.IndexByte(s, ':')
	return colon > 0 && validScheme(s[:colon]) && strings.HasPrefix(s[colon+1:], "//")
}

// validScheme reports whether s matches RFC 3986 ALPHA *( ALPHA / DIGIT / "+" / "-" / "." ).
func validScheme(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !isAlpha && (i == 0 || !(c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.')) {
			return false
		}
	}
	return true
}

// canonicalAuthority lower-cases the host (ASCII only), drops one trailing
// dot and the scheme's default port, and rejects credentials, which must
// never become part of a shared identity, and backslashes, which browsers
// read as path separators. Non-ASCII (IDN) hosts are kept byte for byte:
// no Unicode case mapping or punycode conversion takes place.
func canonicalAuthority(authority, scheme string) (string, error) {
	if strings.Contains(authority, "@") {
		return "", fmt.Errorf("%w: credentials in url", ErrInvalidIdentityURL)
	}
	if strings.Contains(authority, `\`) {
		return "", fmt.Errorf("%w: backslash in authority", ErrInvalidIdentityURL)
	}
	host, port, err := splitHostPort(authority)
	if err != nil {
		return "", err
	}
	host = strings.TrimSuffix(asciiLower(host), ".")
	if host == "" || host == "[]" {
		return "", fmt.Errorf("%w: missing host", ErrInvalidIdentityURL)
	}
	if port == "" {
		return host, nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || strings.Trim(port, "0123456789") != "" {
		return "", fmt.Errorf("%w: invalid port %q", ErrInvalidIdentityURL, port)
	}
	if (scheme == "http" && n == 80) || (scheme == "https" && n == 443) {
		return host, nil
	}
	return host + ":" + strconv.Itoa(n), nil
}

// splitHostPort separates "host", "host:port", "[v6]" and "[v6]:port".
// An empty port after the colon counts as no port.
func splitHostPort(authority string) (string, string, error) {
	if strings.HasPrefix(authority, "[") {
		end := strings.IndexByte(authority, ']')
		if end < 0 {
			return "", "", fmt.Errorf("%w: unterminated ipv6 host", ErrInvalidIdentityURL)
		}
		host, rest := authority[:end+1], authority[end+1:]
		if rest != "" && !strings.HasPrefix(rest, ":") {
			return "", "", fmt.Errorf("%w: invalid authority", ErrInvalidIdentityURL)
		}
		return host, strings.TrimPrefix(rest, ":"), nil
	}
	host, port, _ := strings.Cut(authority, ":")
	if strings.Contains(port, ":") {
		return "", "", fmt.Errorf("%w: invalid authority", ErrInvalidIdentityURL)
	}
	return host, port, nil
}

// canonicalPathQuery drops the fragment and an empty query, defaults an
// empty path to "/", percent-encodes non-ASCII bytes and upper-cases
// percent-escape hex digits, so "café" and "caf%c3%a9" give one key.
func canonicalPathQuery(tail string) string {
	tail, _, _ = strings.Cut(tail, "#")
	path, query, _ := strings.Cut(tail, "?")
	if path == "" {
		path = "/"
	}
	out := upperPercentHex(path)
	if query != "" {
		out += "?" + upperPercentHex(query)
	}
	return out
}

// upperPercentHex upper-cases the two hex digits of every "%xx" escape and
// writes every non-ASCII byte as an upper-case "%XX" escape.
func upperPercentHex(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			b.WriteByte('%')
			b.WriteByte(upperHex(s[i+1]))
			b.WriteByte(upperHex(s[i+2]))
			i += 2
		case c >= 0x80:
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0x0f])
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func upperHex(c byte) byte {
	if c >= 'a' && c <= 'f' {
		return c - 'a' + 'A'
	}
	return c
}

// asciiLower lower-cases A-Z only, so every client gets the same bytes
// regardless of its Unicode case-mapping tables.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// trimASCIISpace trims space, tab, CR, LF, FF and VT; Unicode spaces are
// identity bytes like any other.
func trimASCIISpace(s string) string {
	return strings.Trim(s, " \t\r\n\f\v")
}
