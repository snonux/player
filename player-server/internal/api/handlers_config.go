package api

import (
	"net/http"

	"codeberg.org/snonux/player/internal"
)

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	pageSize := internal.DefaultMediaPageSize
	if s.cfg != nil && s.cfg.MediaPageSize > 0 {
		pageSize = s.cfg.MediaPageSize
	}
	writeJSON(w, http.StatusOK, map[string]int{
		"media_page_size": pageSize,
	})
}
