package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"codeberg.org/snonux/player/internal/service"
	"codeberg.org/snonux/player/internal/transcode"
)

const (
	// compatRetryAfterSeconds is the Retry-After hint sent while a rendition
	// is still being produced.
	compatRetryAfterSeconds = 5
	// statusClientClosedRequest is the de-facto (nginx) status for a request
	// the client abandoned. Nobody reads the response; it only keeps access
	// logs and tests from recording a misleading 200 or 500.
	statusClientClosedRequest = 499
)

// ------------------------------------------------------------------
// Compatibility stream (server-side transcoded rendition)
// ------------------------------------------------------------------

// handleCompatStream serves the H.264/AAC (video) or AAC (audio) rendition of
// a media item to an authenticated user. Access rules equal handleStream.
func (s *Server) handleCompatStream(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Compat) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	rendition, err := s.media.Compat.CompatStream(r.Context(), id, userIDFromContext(r))
	if err != nil {
		s.writeCompatError(w, r, err)
		return
	}
	s.serveRendition(w, r, rendition)
}

// handleShareCompatStream is the public share equivalent of
// handleCompatStream. Like the other share routes it is never cached by
// intermediaries, because the token is the only credential.
func (s *Server) handleShareCompatStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !requireService(w, s.media.Compat) {
		return
	}
	rendition, err := s.media.Compat.SharedCompatStream(r.Context(), r.PathValue("token"))
	if err != nil {
		s.writeCompatError(w, r, err)
		return
	}
	s.serveRendition(w, r, rendition)
}

// writeCompatError maps compat-stream errors to responses.
//
// A pending transcode answers 503 with Retry-After so players and API clients
// know to come back instead of treating it as a broken file. Unexpected
// errors (ffmpeg failures) are logged in full but answered with a generic
// message, because ffmpeg diagnostics contain server file paths.
func (s *Server) writeCompatError(w http.ResponseWriter, r *http.Request, err error) {
	var statuser HTTPStatuser
	switch {
	case errors.Is(err, service.ErrTranscodePending):
		w.Header().Set("Retry-After", strconv.Itoa(compatRetryAfterSeconds))
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":               err.Error(),
			"status":              "transcoding",
			"retry_after_seconds": compatRetryAfterSeconds,
		})
	case errors.Is(err, service.ErrShareExpired):
		writeError(w, http.StatusGone, "gone")
	case errors.As(err, &statuser):
		handleError(w, err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeError(w, statusClientClosedRequest, "request cancelled")
	default:
		s.logger.Error("compat stream", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "transcode failed")
	}
}

// serveRendition streams a finished rendition with Range support.
//
// It does not go through serveFileResult/MediaStreamer: those confine paths
// to the media root, while renditions live in the transcode cache. The ETag
// comes from the rendition (stable per source version) and no Last-Modified
// is sent, because the cache bumps the file's mtime on every access for LRU
// bookkeeping and a changing validator would break If-Range seeking.
func (s *Server) serveRendition(w http.ResponseWriter, r *http.Request, rendition *transcode.Rendition) {
	if rendition == nil {
		notFound(w)
		return
	}
	f, err := os.Open(rendition.Path)
	if err != nil {
		// Evicted between Ensure and Open; the next request re-creates it.
		s.logger.Warn("compat stream open failed", "path", rendition.Path, "err", err)
		notFound(w)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", rendition.ContentType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("ETag", fmt.Sprintf("%q", rendition.ETag))
	s.logger.Info("api compat stream", "rendition", filepath.Base(rendition.Path), "range", r.Header.Get("Range"))
	http.ServeContent(w, r, "", time.Time{}, f)
}
