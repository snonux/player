package api

import (
	"errors"
	"fmt"
	"log/slog"
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

	// compatStatusTranscoding and compatStatusBusy say why a 503 was sent:
	// the rendition is being produced, or the transcoder cannot take the
	// job yet. Clients retry in both cases.
	compatStatusTranscoding = "transcoding"
	compatStatusBusy        = "busy"
	// compatStatusHeader carries that status as a response header, because
	// clients probe readiness with HEAD and a HEAD response has no body.
	compatStatusHeader = "X-Transcode-Status"
)

// ------------------------------------------------------------------
// Compatibility stream (server-side transcoded rendition)
// ------------------------------------------------------------------

// handleCompatStream serves the H.264/AAC (video) or AAC (audio) rendition of
// a media item to an authenticated user. Access rules equal handleStream.
//
// It answers GET and HEAD (the mux routes HEAD to GET patterns). HEAD is the
// clients' readiness probe: it runs the same checks and starts or joins the
// transcode exactly like GET, and http.ServeContent leaves out the body.
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
	f, ok := s.openRendition(w, rendition)
	if !ok {
		return
	}
	defer func() { _ = f.Close() }()
	s.serveRendition(w, r, rendition, f)
}

// handleShareCompatStream is the public share equivalent of
// handleCompatStream. Like the other share routes it is never cached by
// intermediaries, because the token is the only credential.
//
// Share uses are counted per viewing (see service/share_viewing.go). A
// request with a valid viewing credential never costs a use. One without
// opens a viewing of its own, but only for a GET and only after the
// rendition file has been opened: from then on content is certain to be
// delivered. A HEAD probe, or a rendition that was evicted before it could
// be opened (503), never costs a use.
func (s *Server) handleShareCompatStream(w http.ResponseWriter, r *http.Request) {
	preparePublicShare(w)
	if !requireService(w, s.media.Compat) {
		return
	}
	access := shareAccess(r)
	rendition, err := s.media.Compat.SharedCompatStream(r.Context(), access.Token, access.Credential)
	if err != nil {
		s.writeCompatError(w, r, err)
		return
	}
	f, ok := s.openRendition(w, rendition)
	if !ok {
		return
	}
	defer func() { _ = f.Close() }()
	if !access.Probe {
		viewing, err := s.media.Compat.EnsureShareViewing(r.Context(), access.Token, access.Credential)
		if err != nil {
			s.writeCompatError(w, r, err)
			return
		}
		s.handOverShareViewing(w, access.Token, viewing)
	}
	s.serveRendition(w, r, rendition, f)
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
		s.logCompatFailure(r, err)
		writeError(w, http.StatusInternalServerError, "transcode failed")
	}
}

// logCompatFailure logs an unexpected compat-stream error.
//
// Answers that come straight from the transcoder's negative cache or from a
// stopped job are logged at debug level only: no work was done for them, the
// transcoder logged the underlying failure once when it happened, and this
// endpoint can be polled anonymously through a share link — an Error line
// per request would let anyone flood the log.
func (s *Server) logCompatFailure(r *http.Request, err error) {
	level := slog.LevelError
	if errors.Is(err, transcode.ErrFailedRecently) || errors.Is(err, transcode.ErrAborted) {
		level = slog.LevelDebug
	}
	s.logger.Log(r.Context(), level, "compat stream", "path", r.URL.Path, "err", err)
}

// writeCompatRetry answers 503 with Retry-After, so players and API clients
// know to come back instead of treating the item as broken. The reason is
// sent both as a header (for HEAD probes) and in the JSON body.
func writeCompatRetry(w http.ResponseWriter, status, message string) {
	w.Header().Set(compatStatusHeader, status)
	w.Header().Set("Retry-After", strconv.Itoa(compatRetryAfterSeconds))
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error":               message,
		"status":              status,
		"retry_after_seconds": compatRetryAfterSeconds,
	})
}

// openRendition opens a finished rendition file. On failure it has written
// the response and returns false.
//
// It does not go through serveFileResult/MediaStreamer: those confine paths
// to the media root, while renditions live in the transcode cache.
func (s *Server) openRendition(w http.ResponseWriter, rendition *transcode.Rendition) (*os.File, bool) {
	if rendition == nil {
		notFound(w)
		return nil, false
	}
	f, err := os.Open(rendition.Path)
	if err != nil {
		// Evicted between the service call and here. The next request
		// produces it again, so tell the client to retry; a 404 would make
		// players give up on a file that is merely not cached right now.
		s.logger.Warn("compat stream open failed", "rendition", filepath.Base(rendition.Path), "err", err)
		writeCompatRetry(w, compatStatusTranscoding, service.ErrTranscodePending.Error())
		return nil, false
	}
	return f, true
}

// serveRendition streams an opened rendition with Range support. The ETag
// comes from the rendition (it identifies the exact encode) and no
// Last-Modified is sent, so the ETag is the only validator.
func (s *Server) serveRendition(w http.ResponseWriter, r *http.Request, rendition *transcode.Rendition, f *os.File) {
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
