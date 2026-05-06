package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/service"
)

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("encode json", "err", err)
	}
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

func readJSON(r *http.Request, dst interface{}) error {
	if r.Body == nil {
		return errors.New("missing body")
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst)
}

func pathID(r *http.Request, name string) int64 {
	id, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id
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
	http.ServeContent(w, r, filename, stat.ModTime(), f.(io.ReadSeeker))
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
	streamer := s.streamer
	if streamer == nil {
		streamer = service.NewMediaStreamer(nil)
	}

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
