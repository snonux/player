// Package subsonic implements the Subsonic/OpenSubsonic REST protocol
// envelope, authentication helpers, and library response types used by
// Player's optional A01a compatibility surface under /rest.
package subsonic

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// APIVersion is the Subsonic REST protocol version Player advertises.
const APIVersion = "1.16.1"

// ServerType is the OpenSubsonic type field (server product name).
const ServerType = "Player"

// Error codes from the Subsonic/OpenSubsonic contract.
const (
	ErrGeneric                  = 0
	ErrMissingParameter         = 10
	ErrClientMustUpgrade        = 20
	ErrServerMustUpgrade        = 30
	ErrBadCredentials           = 40
	ErrTokenAuthUnsupported     = 41
	ErrAuthMechanismUnsupported = 42
	ErrNotAuthorized            = 50
	ErrTrialExpired             = 60
	ErrDataNotFound             = 70
)

// Error is the nested Subsonic error object.
type Error struct {
	Code    int    `json:"code" xml:"code,attr"`
	Message string `json:"message" xml:"message,attr"`
}

// Extension describes one OpenSubsonic capability.
type Extension struct {
	Name     string `json:"name" xml:"name,attr"`
	Versions []int  `json:"versions" xml:"versions"`
}

// Response is the common subsonic-response wrapper.
type Response struct {
	XMLName                xml.Name       `xml:"subsonic-response" json:"-"`
	Status                 string         `xml:"status,attr" json:"status"`
	Version                string         `xml:"version,attr" json:"version"`
	Type                   string         `xml:"type,attr" json:"type"`
	ServerVersion          string         `xml:"serverVersion,attr" json:"serverVersion"`
	OpenSubsonic           bool           `xml:"openSubsonic,attr" json:"openSubsonic"`
	Error                  *Error         `xml:"error,omitempty" json:"error,omitempty"`
	License                *License       `xml:"license,omitempty" json:"license,omitempty"`
	MusicFolders           *MusicFolders  `xml:"musicFolders,omitempty" json:"musicFolders,omitempty"`
	Indexes                *Indexes       `xml:"indexes,omitempty" json:"indexes,omitempty"`
	Artists                *Artists       `xml:"artists,omitempty" json:"artists,omitempty"`
	Artist                 *Artist        `xml:"artist,omitempty" json:"artist,omitempty"`
	Album                  *Album         `xml:"album,omitempty" json:"album,omitempty"`
	Song                   *Song          `xml:"song,omitempty" json:"song,omitempty"`
	SearchResult2          *SearchResult2 `xml:"searchResult2,omitempty" json:"searchResult2,omitempty"`
	SearchResult3          *SearchResult3 `xml:"searchResult3,omitempty" json:"searchResult3,omitempty"`
	OpenSubsonicExtensions []Extension    `xml:"openSubsonicExtensions,omitempty" json:"openSubsonicExtensions,omitempty"`
}

// License is returned by getLicense.
type License struct {
	Valid bool `json:"valid" xml:"valid,attr"`
}

// MusicFolders wraps musicFolder entries.
type MusicFolders struct {
	Folder []MusicFolder `json:"musicFolder" xml:"musicFolder"`
}

// MusicFolder is a top-level library root (Player set).
type MusicFolder struct {
	ID   string `json:"id" xml:"id,attr"`
	Name string `json:"name" xml:"name,attr"`
}

// Indexes is the file-oriented artist index (getIndexes).
type Indexes struct {
	LastModified    int64   `json:"lastModified" xml:"lastModified,attr"`
	IgnoredArticles string  `json:"ignoredArticles" xml:"ignoredArticles,attr"`
	Index           []Index `json:"index" xml:"index"`
}

// Index groups artists by letter.
type Index struct {
	Name   string   `json:"name" xml:"name,attr"`
	Artist []Artist `json:"artist" xml:"artist"`
}

// Artists is the ID3 artist index (getArtists).
type Artists struct {
	IgnoredArticles string  `json:"ignoredArticles" xml:"ignoredArticles,attr"`
	Index           []Index `json:"index" xml:"index"`
}

// Artist is an artist entry used in indexes and getArtist.
type Artist struct {
	ID         string  `json:"id" xml:"id,attr"`
	Name       string  `json:"name" xml:"name,attr"`
	AlbumCount int     `json:"albumCount,omitempty" xml:"albumCount,attr,omitempty"`
	CoverArt   string  `json:"coverArt,omitempty" xml:"coverArt,attr,omitempty"`
	Album      []Album `json:"album,omitempty" xml:"album"`
}

// Album is an album entry used in getArtist/getAlbum/search.
type Album struct {
	ID        string `json:"id" xml:"id,attr"`
	Name      string `json:"name" xml:"name,attr"`
	Artist    string `json:"artist,omitempty" xml:"artist,attr,omitempty"`
	ArtistID  string `json:"artistId,omitempty" xml:"artistId,attr,omitempty"`
	CoverArt  string `json:"coverArt,omitempty" xml:"coverArt,attr,omitempty"`
	SongCount int    `json:"songCount,omitempty" xml:"songCount,attr,omitempty"`
	Duration  int    `json:"duration,omitempty" xml:"duration,attr,omitempty"`
	Year      int    `json:"year,omitempty" xml:"year,attr,omitempty"`
	Created   string `json:"created,omitempty" xml:"created,attr,omitempty"`
	Song      []Song `json:"song,omitempty" xml:"song"`
}

// Song is a track entry.
type Song struct {
	ID          string `json:"id" xml:"id,attr"`
	Parent      string `json:"parent,omitempty" xml:"parent,attr,omitempty"`
	IsDir       bool   `json:"isDir" xml:"isDir,attr"`
	Title       string `json:"title" xml:"title,attr"`
	Album       string `json:"album,omitempty" xml:"album,attr,omitempty"`
	Artist      string `json:"artist,omitempty" xml:"artist,attr,omitempty"`
	Track       int    `json:"track,omitempty" xml:"track,attr,omitempty"`
	Year        int    `json:"year,omitempty" xml:"year,attr,omitempty"`
	Genre       string `json:"genre,omitempty" xml:"genre,attr,omitempty"`
	CoverArt    string `json:"coverArt,omitempty" xml:"coverArt,attr,omitempty"`
	Size        int64  `json:"size,omitempty" xml:"size,attr,omitempty"`
	ContentType string `json:"contentType,omitempty" xml:"contentType,attr,omitempty"`
	Suffix      string `json:"suffix,omitempty" xml:"suffix,attr,omitempty"`
	Duration    int    `json:"duration,omitempty" xml:"duration,attr,omitempty"`
	BitRate     int    `json:"bitRate,omitempty" xml:"bitRate,attr,omitempty"`
	Path        string `json:"path,omitempty" xml:"path,attr,omitempty"`
	IsVideo     bool   `json:"isVideo" xml:"isVideo,attr"`
	Created     string `json:"created,omitempty" xml:"created,attr,omitempty"`
	AlbumID     string `json:"albumId,omitempty" xml:"albumId,attr,omitempty"`
	ArtistID    string `json:"artistId,omitempty" xml:"artistId,attr,omitempty"`
	Type        string `json:"type,omitempty" xml:"type,attr,omitempty"`
	DiscNumber  int    `json:"discNumber,omitempty" xml:"discNumber,attr,omitempty"`
}

// SearchResult2 is the search2 payload.
type SearchResult2 struct {
	Artist []Artist `json:"artist" xml:"artist"`
	Album  []Album  `json:"album" xml:"album"`
	Song   []Song   `json:"song" xml:"song"`
}

// SearchResult3 is the search3 payload.
type SearchResult3 struct {
	Artist []Artist `json:"artist" xml:"artist"`
	Album  []Album  `json:"album" xml:"album"`
	Song   []Song   `json:"song" xml:"song"`
}

// OK builds a successful empty response envelope.
func OK(serverVersion string) *Response {
	return &Response{
		Status:        "ok",
		Version:       APIVersion,
		Type:          ServerType,
		ServerVersion: serverVersion,
		OpenSubsonic:  true,
	}
}

// Fail builds a failed response envelope.
func Fail(serverVersion string, code int, message string) *Response {
	return &Response{
		Status:        "failed",
		Version:       APIVersion,
		Type:          ServerType,
		ServerVersion: serverVersion,
		OpenSubsonic:  true,
		Error:         &Error{Code: code, Message: message},
	}
}

// SupportedExtensions lists OpenSubsonic extensions implemented in A01a.
func SupportedExtensions() []Extension {
	return []Extension{
		{Name: "apiKeyAuthentication", Versions: []int{1}},
		{Name: "formPost", Versions: []int{1}},
	}
}

// FormatFromRequest returns "json" or "xml" (default xml per Subsonic).
func FormatFromRequest(r *http.Request) string {
	f := strings.ToLower(strings.TrimSpace(firstQuery(r, "f")))
	if f == "json" || f == "jsonp" {
		return "json"
	}
	return "xml"
}

// Write encodes resp according to the client's f parameter.
func Write(w http.ResponseWriter, r *http.Request, resp *Response) error {
	format := FormatFromRequest(r)
	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		return enc.Encode(map[string]*Response{"subsonic-response": resp})
	default:
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		if _, err := w.Write([]byte(xml.Header)); err != nil {
			return err
		}
		return xml.NewEncoder(w).Encode(resp)
	}
}

// ParseIntParam reads an optional integer query/form value.
func ParseIntParam(r *http.Request, name string, fallback int) (int, error) {
	raw := strings.TrimSpace(firstQuery(r, name))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return n, nil
}

func firstQuery(r *http.Request, name string) string {
	if err := r.ParseForm(); err == nil {
		if v := r.Form.Get(name); v != "" {
			return v
		}
	}
	return r.URL.Query().Get(name)
}
