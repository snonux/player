package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"codeberg.org/snonux/player/internal/service"
)

func (s *Server) handleMusicDownloads(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	var request struct {
		MediaIDs []int64 `json:"media_ids"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		badRequest(w, "invalid music download request")
		return
	}
	archive, err := service.BuildMusicDownloadArchive(r.Context(), s.media.Browse, s.streamer, request.MediaIDs, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	defer func() { _ = archive.File.Close(); _ = os.Remove(archive.File.Name()) }()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="Player-music.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Download-Count", strconv.Itoa(archive.Count))
	http.ServeContent(w, r, "Player-music.zip", time.Time{}, archive.File)
}
