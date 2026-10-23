package api

import (
	"github.com/snonux/player/internal/service"
	"context"
	"net/http"
)

func (s *Server) handleMusicFavorite(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	favorite := r.Method == http.MethodPut
	err := service.SetMusicFavorite(r.Context(), s.store, s.media.Browse, s.media.Playlists, userIDFromContext(r), r.PathValue("kind"), r.PathValue("entity_key"), favorite)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"is_favorite": favorite})
}

// musicFavorites permits partial read-only server fixtures without persistence.
func (s *Server) musicFavorites(ctx context.Context, user int64, kind string) (map[string]bool, error) {
	if s.store == nil {
		return map[string]bool{}, nil
	}
	return s.store.ListMusicFavorites(ctx, user, kind)
}
