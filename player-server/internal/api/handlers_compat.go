package api

import (
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
	// is not available yet.
	compatRetryAfterSeconds = 5
	// statusClientClosedRequest is the de-facto (nginx) status for a request
	// the client abandoned. Nobody reads the response; it only keeps access
	// logs and tests from recording a misleading 200 or 500.
	statusClientClosedRequest = 499

	// compatStatusTranscoding and compatStatusBusy are the "status" values
	// of a 503 body: the rendition is being produced, or the transcoder
	// cannot take the job yet. Clients retry in both cases.
	compatStatusTranscoding = "transcoding"
	compatStatusBusy        = "busy"
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
// The body never contains err.Error() of the whole chain: transcode errors
// carry server file paths and ffmpeg diagnostics, and this endpoint is also
// reachable anonymously through share links. Service sentinels answer with
// their own fixed text; everything else is logged in full and answered with
// a generic message.
func (s *Server) writeCompatError(w http.ResponseWriter, r *http.Request, err error) {
	var sentinel interface {
		HTTPStatuser
		error
	}
	switch {
	case r.Context().Err() != nil:
		// Only the request's own context decides this. A job stopped by
		// shutdown or its timeout is a server-side failure, handled below.
		writeError(w, statusClientClosedRequest, "request cancelled")
	case errors.Is(err, service.ErrShareExpired):
		writeError(w, http.StatusGone, "gone")
	case errors.Is(err, service.ErrTranscodePending):
		writeCompatRetry(w, compatStatusTranscoding, service.ErrTranscodePending.Error())
	case errors.Is(err, service.ErrTranscodeBusy):
		writeCompatRetry(w, compatStatusBusy, service.ErrTranscodeBusy.Error())
	case errors.As(err, &sentinel):
		writeError(w, sentinel.HTTPStatus(), sentinel.Error())
	default:
		s.logger.Error("compat stream", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "transcode failed")
	}
}

// writeCompatRetry answers 503 with Retry-After, so players and API clients
// know to come back instead of treating the item as broken.
func writeCompatRetry(w http.ResponseWriter, status, message string) {
	w.Header().Set("Retry-After", strconv.Itoa(compatRetryAfterSeconds))
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error":               message,
		"status":              status,
		"retry_after_seconds": compatRetryAfterSeconds,
	})
}

// serveRendition streams a finished rendition with Range support.
//
// It does not go through serveFileResult/MediaStreamer: those confine paths
// to the media root, while renditions live in the transcode cache. The ETag
// comes from the rendition (it identifies the exact encode) and no
// Last-Modified is sent, so the ETag is the only validator.
func (s *Server) serveRendition(w http.ResponseWriter, r *http.Request, rendition *transcode.Rendition) {
	if rendition == nil {
		notFound(w)
		return
	}
	f, err := os.Open(rendition.Path)
	if err != nil {
		// Evicted between the service call and here. The next request
		// produces it again, so tell the client to retry; a 404 would make
		// players give up on a file that is merely not cached right now.
		s.logger.Warn("compat stream open failed", "rendition", filepath.Base(rendition.Path), "err", err)
		writeCompatRetry(w, compatStatusTranscoding, service.ErrTranscodePending.Error())
		return
	}
	defer func() { _ = f.Close() }()

	// The handler may have spent up to 20 s waiting for the transcode, and
	// the server's write deadline has been running since the request was
	// read. Restart it so the body gets the same time budget a /stream
	// response has, instead of being cut after the few seconds left.
	// Writers that cannot set deadlines (tests) just keep the default.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(httpWriteTimeout))

	w.Header().Set("Content-Type", rendition.ContentType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("ETag", fmt.Sprintf("%q", rendition.ETag))
	s.logger.Info("api compat stream", "rendition", filepath.Base(rendition.Path), "range", r.Header.Get("Range"))
	http.ServeContent(w, r, "", time.Time{}, f)
}
