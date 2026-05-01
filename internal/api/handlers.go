package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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

	if !attachment && looksLikeMPEGTS(res.Path) {
		s.serveRemuxedMP4(w, r, res, stat.Size())
		return
	}

	if attachment {
		disp := fmt.Sprintf("attachment; filename=%q", res.FileName)
		w.Header().Set("Content-Disposition", disp)
	}
	// Set Content-Type so browsers know how to decode the file without
	// needing to sniff, which avoids buffering delays during streaming.
	w.Header().Set("Content-Type", mimeTypeForFilename(res.FileName))
	w.Header().Set("Accept-Ranges", "bytes")
	fmt.Printf("[api] stream file=%s size=%d bytes range=%s\n", res.FileName, stat.Size(), r.Header.Get("Range"))
	http.ServeContent(w, r, res.FileName, stat.ModTime(), f)
}

func (s *Server) serveRemuxedMP4(w http.ResponseWriter, r *http.Request, res *service.FileResult, size int64) {
	cmd := exec.CommandContext(
		r.Context(),
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-i", res.Path,
		"-map", "0:v:0?",
		"-map", "0:a:0?",
		"-dn",
		"-sn",
		"-c", "copy",
		"-bsf:a", "aac_adtstoasc",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-f", "mp4",
		"pipe:1",
	)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "stream setup failed", http.StatusInternalServerError)
		return
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, "stream setup failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Printf("[api] remux stream file=%s size=%d bytes range=%s\n", res.FileName, size, r.Header.Get("Range"))
	if _, err := io.Copy(w, stdout); err != nil && r.Context().Err() == nil {
		slog.Error("copy remuxed media", "file", res.FileName, "err", err)
	}
	if err := cmd.Wait(); err != nil && r.Context().Err() == nil {
		slog.Error("remux media", "file", res.FileName, "err", err)
	}
}

func looksLikeMPEGTS(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 188*5)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false
	}
	buf = buf[:n]
	return hasMPEGTSsync(buf, 188) || hasMPEGTSsync(buf, 192)
}

func hasMPEGTSsync(buf []byte, packetSize int) bool {
	if len(buf) < packetSize*3+1 {
		return false
	}
	for offset := 0; offset < packetSize; offset++ {
		matches := 0
		for pos := offset; pos < len(buf); pos += packetSize {
			if buf[pos] != 0x47 {
				break
			}
			matches++
			if matches >= 3 {
				return true
			}
		}
	}
	return false
}

// mimeTypeForFilename returns an HTTP Content-Type based on the file extension.
func mimeTypeForFilename(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	t := mime.TypeByExtension(ext)
	if t != "" {
		return t
	}
	switch ext {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".wav":
		return "audio/wav"
	case ".aac", ".m4a":
		return "audio/mp4"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".m4b":
		return "audio/x-m4b"
	}
	return "application/octet-stream"
}
