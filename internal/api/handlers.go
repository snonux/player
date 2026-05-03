package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/probe"
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

func readJSON(r *http.Request, dst interface{}) error {
	if r.Body == nil {
		return fmt.Errorf("missing body")
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
	f, err := os.Open(res.Path)
	if err != nil {
		fmt.Printf("[api] stream file=%s error=open_failed\n", res.FileName)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		fmt.Printf("[api] stream file=%s error=stat_failed\n", res.FileName)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if !attachment && probe.LooksLikeMPEGTS(res.Path) {
		s.serveRemuxedMP4(w, r, res, stat.Size())
		return
	}

	if attachment {
		disp := fmt.Sprintf("attachment; filename=%q", res.FileName)
		w.Header().Set("Content-Disposition", disp)
	}
	// Set Content-Type so browsers know how to decode the file without
	// needing to sniff, which avoids buffering delays during streaming.
	w.Header().Set("Content-Type", probe.MimeTypeForFilename(res.FileName))
	w.Header().Set("Accept-Ranges", "bytes")
	fmt.Printf("[api] stream file=%s size=%d bytes range=%s\n", res.FileName, stat.Size(), r.Header.Get("Range"))
	http.ServeContent(w, r, res.FileName, stat.ModTime(), f)
}

func (s *Server) serveRemuxedMP4(w http.ResponseWriter, r *http.Request, res *service.FileResult, size int64) {
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Printf("[api] remux stream file=%s size=%d bytes range=%s\n", res.FileName, size, r.Header.Get("Range"))
	if err := s.remuxer.Remux(r.Context(), res.Path, w); err != nil {
		slog.Error("remux media", "file", res.FileName, "err", err)
	}
}
