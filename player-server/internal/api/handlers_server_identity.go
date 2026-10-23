package api

import (
	"net/http"

	"codeberg.org/snonux/player/internal/repository"
)

// handleServerIdentity is credential-free so clients can test a paired address
// without disclosing account credentials to an unrelated installation.
func (s *Server) handleServerIdentity(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.store.(repository.ServerIdentityRepo)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "server identity unavailable"})
		return
	}
	identity, err := repo.ServerIdentity(r.Context())
	if err != nil {
		handleError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"server_id": identity})
}
