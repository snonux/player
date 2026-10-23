package api

import (
	"fmt"
	"mime"
	"net/http"
)

func (s *Server) handlePlaylistExport(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Playlists) {
		return
	}
	data, err := s.media.Playlists.ExportM3U(r.Context(), userIDFromContext(r), r.PathValue("playlist_id"))
	if err != nil {
		handleError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl; charset=utf-8")
	// The fixed filename is independent of user-controlled playlist metadata.
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "playlist-" + r.PathValue("playlist_id") + ".m3u8"}))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := fmt.Fprint(w, data); err != nil {
		s.logger.Debug("write playlist export", "error", err)
	}
}
