package api

import (
	"codeberg.org/snonux/player/internal/service"
	"net/http"
	"strconv"
)

func (s *Server) handleMusicMix(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	kind := r.PathValue("kind")
	if kind != "song" && kind != "album" && kind != "artist" {
		writeError(w, http.StatusBadRequest, "Choose song, album or artist.")
		return
	}
	minimum := 0
	if value, supplied := r.URL.Query()["min_rating"]; supplied {
		var err error
		if len(value) != 1 {
			writeError(w, 400, "Choose a minimum rating from 0 to 5.")
			return
		}
		minimum, err = strconv.Atoi(value[0])
		if err != nil || minimum < 0 || minimum > 5 {
			writeError(w, 400, "Choose a minimum rating from 0 to 5.")
			return
		}
	}
	albums, err := service.MusicAlbums(r.Context(), s.media.Browse, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	if _, err = s.rateMusic(r.Context(), userIDFromContext(r), albums); err != nil {
		handleError(w, err)
		return
	}
	result, found := service.BuildMusicMix(albums, kind, r.PathValue("entity_key"), minimum)
	if !found {
		writeError(w, http.StatusNotFound, "Music is unavailable or you no longer have permission.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
