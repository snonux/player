package api

import (
	"codeberg.org/snonux/player/internal/service"
	"net/http"
)

func (s *Server) handleMusicEnrichment(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	kind := r.PathValue("kind")
	if kind != "album" && kind != "artist" {
		writeError(w, http.StatusBadRequest, "Enrichment is available for albums and artists.")
		return
	}
	albums, err := service.MusicAlbums(r.Context(), s.media.Browse, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	id, found := service.MusicEnrichmentIdentity(albums, kind, r.PathValue("entity_key"))
	if !found {
		writeError(w, http.StatusNotFound, "Music is unavailable or you no longer have permission.")
		return
	}
	user, err := s.store.GetUserByID(r.Context(), userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	if user == nil {
		writeError(w, http.StatusUnauthorized, "Sign in to read music metadata.")
		return
	}
	refresh := r.Method == http.MethodPost
	if refresh && !user.IsAdmin {
		writeError(w, http.StatusForbidden, "Only administrators can refresh shared music metadata.")
		return
	}
	var provider service.EnrichmentProvider
	if s.cfg.MusicEnrichmentURL != "" {
		provider = service.NewHTTPEnrichmentProvider(s.cfg.MusicEnrichmentURL)
	}
	result, err := service.LoadMusicEnrichment(r.Context(), s.store, provider, s.cfg.MusicEnrichmentURL, id, refresh, user.IsAdmin, s.clk.Now())
	if err != nil {
		handleError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}
