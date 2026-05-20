package api

import (
	"fmt"
	"net/http"
	"time"

	"codeberg.org/snonux/player/internal/service"
)

// fileETag returns a strong ETag value (without surrounding quotes) for a
// file of the given size and modification time. Combining size with mtime
// nanoseconds is enough to detect any in-place rewrite or replacement —
// callers wrap the result in quotes when emitting the header.
func fileETag(size int64, modTime time.Time) string {
	return fmt.Sprintf("%d-%d", size, modTime.UnixNano())
}

// serveFileResult opens a stream for the given FileResult and writes it to the
// response. If the file requires remuxing (e.g. format conversion), it
// delegates to serveRemuxed; otherwise it uses http.ServeContent for efficient
// range-request support and ETag-based revalidation.
//
// s.streamer is required at construction time (see NewServerWithLogger), so it
// is guaranteed non-nil here. We previously fell back to a default streamer
// when nil, which silently hid wiring mistakes and violated the Dependency
// Inversion Principle by letting the handler decide its own dependency.
func (s *Server) serveFileResult(w http.ResponseWriter, r *http.Request, res *service.FileResult, attachment bool) {
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

// serveRemuxed writes a remuxed (format-converted) media stream directly to
// the response writer. It sets appropriate headers (Content-Type,
// Cache-Control, and optionally X-Duration) and streams the output of the
// remux operation. Errors during remuxing are logged but cannot be propagated
// as headers may already have been written.
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
