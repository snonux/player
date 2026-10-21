package api

import (
	"net/http"

	"codeberg.org/snonux/player/internal"
)

// podcastSyncVersion is the podcast_sync capability advertised in the
// config (docs/podcast-sync-contract.md §6.1): the server implements
// POST /api/v1/podcasts/sync with subscription and episode registers. A
// client that does not find it runs podcasts standalone for that account.
const podcastSyncVersion = 1

// handleConfig serves the client configuration and capabilities.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	pageSize := internal.DefaultMediaPageSize
	if s.cfg != nil && s.cfg.MediaPageSize > 0 {
		pageSize = s.cfg.MediaPageSize
	}
	writeJSON(w, http.StatusOK, map[string]int64{
		"media_page_size": int64(pageSize),
		"user_id":         userIDFromContext(r),
		"podcast_sync":    podcastSyncVersion,
	})
}
