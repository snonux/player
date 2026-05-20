// Package web renders HTML pages for the player-server.
//
// The share-page renderer encapsulates the templating concern that used
// to live inline in the api package: opening the static share.html file,
// replacing the SHARE_MEDIA placeholder with marshaled JSON metadata,
// and reporting the file's ModTime for cache validators.
//
// Keeping this logic here lets HTTP handlers stay focused on routing and
// error translation (Separation of Concerns) and stops them reaching
// through a file-system abstraction (Law of Demeter).
package web

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"
)

// MaxFileNameLength is the maximum number of runes kept from a media filename
// before it is truncated. Filenames beyond this limit can cause unbounded
// memory growth when embedded in the share-page HTML, so they are silently
// capped here. 255 is a common filesystem limit and a reasonable upper bound
// for display purposes.
const MaxFileNameLength = 255

// ShareMediaPlaceholder is the HTML comment that gets substituted with
// the JSON-encoded share metadata inside share.html.
const ShareMediaPlaceholder = "<!--SHARE_MEDIA-->"

// shareTemplateName is the filename looked up in the static FS.
const shareTemplateName = "share.html"

// SharePageRenderer renders the public share landing page by inlining
// share metadata into a static HTML template.
//
// A renderer captures the file system and the placeholder it works with
// so callers (HTTP handlers) only need to pass the data to inject.
type SharePageRenderer struct {
	fs          http.FileSystem
	template    string
	placeholder string
}

// NewSharePageRenderer builds a renderer backed by the given file system.
// The file system must contain share.html. The placeholder defaults to
// ShareMediaPlaceholder.
func NewSharePageRenderer(fs http.FileSystem) *SharePageRenderer {
	return &SharePageRenderer{
		fs:          fs,
		template:    shareTemplateName,
		placeholder: ShareMediaPlaceholder,
	}
}

// RenderedPage carries the bytes to serve along with the source template's
// modification time (used for HTTP cache validators in ServeContent).
type RenderedPage struct {
	HTML    string
	ModTime time.Time
	Name    string
}

// Render reads the share template from the file system, injects the
// JSON-encoded data in place of the placeholder, and returns the result.
//
// The caller (an HTTP handler) is responsible for turning errors into
// appropriate HTTP status codes; this package stays transport-agnostic.
func (r *SharePageRenderer) Render(data any) (RenderedPage, error) {
	if r == nil || r.fs == nil {
		return RenderedPage{}, fmt.Errorf("share renderer not configured")
	}

	f, err := r.fs.Open(r.template)
	if err != nil {
		return RenderedPage{}, fmt.Errorf("open share template: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return RenderedPage{}, fmt.Errorf("stat share template: %w", err)
	}

	var buf strings.Builder
	if _, err := io.Copy(&buf, f); err != nil {
		return RenderedPage{}, fmt.Errorf("read share template: %w", err)
	}

	rendered, err := injectShareMedia(buf.String(), r.placeholder, data)
	if err != nil {
		return RenderedPage{}, err
	}

	return RenderedPage{
		HTML:    rendered,
		ModTime: stat.ModTime(),
		Name:    r.template,
	}, nil
}

// SanitizeFileName truncates s to MaxFileNameLength runes and HTML-escapes
// the result. Both steps protect against DoS via enormous filenames and
// against HTML injection when the filename is embedded in a page attribute
// or element text context outside of the JSON-encoded script block.
func SanitizeFileName(s string) string {
	runes := []rune(s)
	if len(runes) > MaxFileNameLength {
		runes = runes[:MaxFileNameLength]
	}
	return html.EscapeString(string(runes))
}

// injectShareMedia replaces placeholder with the JSON-encoded form of
// data, returning the new HTML. It is private to keep this package's
// surface small: callers are expected to go through SharePageRenderer.
//
// Go's encoding/json already escapes <, > and & as Unicode escapes inside
// string values, so the JSON blob is safe to embed in a <script> tag.
// The explicit SanitizeFileName call on the way in (see handlers_share.go)
// provides a second layer of defence and enforces a length cap.
func injectShareMedia(htmlDoc, placeholder string, data any) (string, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal share metadata: %w", err)
	}
	return strings.Replace(htmlDoc, placeholder, string(encoded), 1), nil
}
