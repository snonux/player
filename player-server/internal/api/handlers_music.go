package api

import (
	"net/http"
	"strconv"

	"codeberg.org/snonux/player/internal/service"
)

func (s *Server) handleMusicAlbums(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	albums, err := service.MusicAlbums(r.Context(), s.media.Browse, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	key, artist := r.PathValue("album_key"), r.URL.Query().Get("artist")
	mediaID, _ := strconv.ParseInt(r.URL.Query().Get("media_id"), 10, 64)
	result := make([]service.Album, 0, len(albums))
	for _, album := range albums {
		if key != "" && album.Key != key {
			continue
		}
		if artist != "" && album.ArtistKey != artist {
			continue
		}
		if mediaID > 0 {
			found := false
			for _, song := range album.Songs {
				if song.ID == mediaID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if key != "" {
			writeJSON(w, http.StatusOK, album)
			return
		}
		album.Discs = nil
		album.Songs = nil
		album.SongCredits = nil
		result = append(result, album)
	}
	if key != "" {
		writeError(w, http.StatusNotFound, "Album is unavailable or you no longer have permission.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleMusicArtists(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	role := r.URL.Query().Get("role")
	switch role {
	case "", "artists", "track_artist", "album_artist", "composer", "performer":
	default:
		writeError(w, http.StatusBadRequest, "Unknown artist role.")
		return
	}
	albums, err := service.MusicAlbums(r.Context(), s.media.Browse, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	result := make([]service.Artist, 0)
	key := r.PathValue("artist_key")
	for _, person := range service.MusicArtists(albums) {
		if key != "" {
			if person.Key == key {
				writeJSON(w, http.StatusOK, person)
				return
			}
			continue
		}
		matches := false
		for _, credited := range person.Roles {
			if credited == role || ((role == "" || role == "artists") && (credited == "track_artist" || credited == "album_artist")) {
				matches = true
			}
		}
		if matches {
			person.Albums = nil
			result = append(result, person)
		}
	}
	if key != "" {
		writeError(w, http.StatusNotFound, "Artist is unavailable or you no longer have permission.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
