package api

import (
	"errors"
	"net/http"

	"codeberg.org/snonux/player/internal/model"
)

// maxPodcastSyncBody bounds the body of a podcast sync request (2 MiB).
const maxPodcastSyncBody = 2 << 20

// handlePodcastSync serves POST /api/podcasts/sync and /api/v1/podcasts/sync:
// it applies the caller's subscription edits and returns the caller's
// changes after the cursor (docs/podcast-sync-contract.md §6.2). Every
// signed-in user may call it; it reads and writes only the caller's rows.
// A body over the limit answers 413, so a client halves its page.
func (s *Server) handlePodcastSync(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.podcastSync) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPodcastSyncBody)
	var req model.PodcastSyncRequest
	if err := readJSON(r, &req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		badRequest(w, "invalid body")
		return
	}
	resp, err := s.podcastSync.Sync(r.Context(), userIDFromContext(r), req)
	if err != nil {
		s.logger.Warn("podcast sync", "err", err)
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
