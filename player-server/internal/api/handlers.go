package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/service"
)

// fileETag returns a strong ETag value (without surrounding quotes) for a
// file of the given size and modification time. Combining size with mtime
// nanoseconds is enough to detect any in-place rewrite or replacement —
// callers wrap the result in quotes when emitting the header.
func fileETag(size int64, modTime time.Time) string {
	return fmt.Sprintf("%d-%d", size, modTime.UnixNano())
}

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

// writeJSON serialises data to JSON and writes it to w with the given status.
//
// Marshalling happens into an in-memory buffer BEFORE any status or body is
// written to the response. This avoids the previous footgun where the headers
// (and a 200/whatever status) were committed first and then encoding failed
// halfway through, producing a response with a misleading success status and a
// truncated/invalid body. If encoding fails we instead emit a 500 with a small
// JSON error payload so callers always see a consistent error shape.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(data); err != nil {
		slog.Error("encode json", "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal encoding error"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func badRequest(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, message)
}

func notFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "not found")
}

func forbidden(w http.ResponseWriter, message string) {
	writeError(w, http.StatusForbidden, message)
}

// handleError maps service sentinel errors to the appropriate HTTP status
// and writes a JSON error response. It falls back to 500 for unknown errors.
func handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound),
		errors.Is(err, service.ErrShareNotFound),
		errors.Is(err, service.ErrMediaNotFound):
		notFound(w)
	case errors.Is(err, service.ErrForbidden):
		forbidden(w, "forbidden")
	case errors.Is(err, service.ErrAlreadyBootstrapped):
		forbidden(w, "bootstrap already complete")
	case errors.Is(err, service.ErrInvalidCredentials):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
	case errors.Is(err, service.ErrUnsupportedExtension),
		errors.Is(err, service.ErrInvalidFeed),
		errors.Is(err, service.ErrCannotDeleteSelf),
		errors.Is(err, service.ErrEmptySetForCover):
		badRequest(w, err.Error())
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func readJSON(r *http.Request, dst interface{}) error {
	if r.Body == nil {
		return errors.New("missing body")
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst)
}

// pathID parses a path variable as an int64. It returns the parsed id together
// with an explicit error so callers can distinguish "missing/malformed" from a
// legitimately zero value and log the underlying ParseInt failure. The error
// is wrapped with the variable name to make server logs actionable.
func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	return id, nil
}

func userIDFromContext(r *http.Request) int64 {
	sess, _ := r.Context().Value(sessionCtxKey).(*model.Session)
	if sess == nil {
		return 0
	}
	return sess.UserID
}

func sessionIDFromContext(r *http.Request) string {
	sess, _ := r.Context().Value(sessionCtxKey).(*model.Session)
	if sess == nil {
		return ""
	}
	return sess.ID
}

func stringPtr(s string) *string { return &s }

func floatPtr(f float64) *float64 { return &f }

func intPtr(i int64) *int64 { return &i }

func requireService(w http.ResponseWriter, svc any) bool {
	if svc == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not implemented"})
		return false
	}
	return true
}

// ------------------------------------------------------------------
// Static pages
// ------------------------------------------------------------------

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, filename string) {
	f, err := s.staticFS.Open(filename)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, filename, stat.ModTime(), rs)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	s.serveFile(w, r, "index.html")
}

func (s *Server) serveLogin(w http.ResponseWriter, r *http.Request) {
	s.serveFile(w, r, "login.html")
}

func (s *Server) serveBootstrap(w http.ResponseWriter, r *http.Request) {
	s.serveFile(w, r, "bootstrap.html")
}

func (s *Server) serveDetach(w http.ResponseWriter, r *http.Request) {
	s.serveFile(w, r, "detach.html")
}

// ------------------------------------------------------------------
// File serving helpers
// ------------------------------------------------------------------

func (s *Server) serveFileResult(w http.ResponseWriter, r *http.Request, res *service.FileResult, attachment bool) {
	// s.streamer is required at construction time (see NewServerWithLogger),
	// so it is guaranteed non-nil here. We previously fell back to a default
	// streamer when nil, which silently hid wiring mistakes and violated the
	// Dependency Inversion Principle by letting the handler decide its own
	// dependency.
	streamer := s.streamer

	stream, err := streamer.Open(r.Context(), res, attachment)
	if err != nil {
		s.logger.Warn("api stream open failed", "file", res.FileName, "err", err)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer stream.File.Close()

	if stream.Remuxed {
		s.serveRemuxed(w, r, streamer, stream)
		return
	}

	if attachment {
		disp := fmt.Sprintf("attachment; filename=%q", res.FileName)
		w.Header().Set("Content-Disposition", disp)
	}
	w.Header().Set("Content-Type", stream.ContentType)
	w.Header().Set("Accept-Ranges", "bytes")
	// Strong ETag derived from size and mtime nanoseconds. http.ServeContent
	// reads If-None-Match / If-Match from the request once ETag is set, so
	// clients (iOS audio player, podcast clients) can revalidate cached
	// downloads without re-fetching the full body.
	w.Header().Set("ETag", fmt.Sprintf("%q", fileETag(stream.Size, stream.ModTime)))
	s.logger.Info("api stream file", "file", stream.FileName, "size", stream.Size, "range", r.Header.Get("Range"))
	http.ServeContent(w, r, stream.FileName, stream.ModTime, stream.File)
}

func (s *Server) serveRemuxed(w http.ResponseWriter, r *http.Request, streamer service.MediaStreamer, stream *service.StreamResult) {
	w.Header().Set("Content-Type", stream.ContentType)
	w.Header().Set("Cache-Control", "no-store")
	if stream.Duration > 0 {
		w.Header().Set("X-Duration", fmt.Sprintf("%f", stream.Duration))
	}
	s.logger.Info("api remux stream file", "file", stream.FileName, "size", stream.Size, "range", r.Header.Get("Range"))
	if err := streamer.Remux(r.Context(), stream, w); err != nil {
		s.logger.Error("remux media", "file", stream.FileName, "err", err)
	}
}
