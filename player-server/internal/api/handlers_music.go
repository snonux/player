package api

import (
	"net/http"
	"strconv"

	"codeberg.org/snonux/player/internal/model"
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
	if key == "" {
		filter, filterErr := service.ParseMusicFilter(r.URL.Query())
		if filterErr != nil {
			writeError(w, http.StatusBadRequest, filterErr.Error())
			return
		}
		albums = service.FilterMusic(albums, filter)
	}
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
	roles := r.URL.Query()["role"]
	if len(roles) == 0 {
		roles = []string{"artists"}
	}
	for _, role := range roles {
		switch role {
		case "", "artists", "track_artist", "album_artist", "composer", "performer":
		default:
			writeError(w, http.StatusBadRequest, "Unknown artist role.")
			return
		}
	}
	albums, err := service.MusicAlbums(r.Context(), s.media.Browse, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	result := make([]service.Artist, 0)
	key := r.PathValue("artist_key")
	if key == "" {
		filter, filterErr := service.ParseMusicFilter(r.URL.Query())
		if filterErr != nil {
			writeError(w, http.StatusBadRequest, filterErr.Error())
			return
		}
		albums = service.FilterMusic(albums, filter)
	}
	for _, person := range service.MusicArtists(albums) {
		if key != "" {
			if person.Key == key {
				writeJSON(w, http.StatusOK, person)
				return
			}
			continue
		}
		matches := false
		for _, role := range roles {
			for _, credited := range person.Roles {
				if credited == role || ((role == "" || role == "artists") && (credited == "track_artist" || credited == "album_artist")) {
					matches = true
				}
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

func (s *Server) handleMusicSearch(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	albums, err := service.MusicAlbums(r.Context(), s.media.Browse, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	query := r.URL.Query()
	filter, filterErr := service.ParseMusicFilter(query)
	if filterErr != nil {
		writeError(w, http.StatusBadRequest, filterErr.Error())
		return
	}
	albums = service.FilterMusic(albums, filter)
	if len(filter) > 0 && query.Get("q") == "" && query.Get("artist") == "" && query.Get("album") == "" {
		result := service.MusicSearchResult{Songs: []model.Media{}, Albums: []service.Album{}, Artists: service.MusicArtists(albums)}
		for _, album := range albums {
			result.Songs = append(result.Songs, album.Songs...)
			album.Songs = nil
			album.Discs = nil
			album.SongCredits = nil
			result.Albums = append(result.Albums, album)
		}
		for i := range result.Artists {
			result.Artists[i].Albums = nil
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	writeJSON(w, http.StatusOK, service.SearchMusic(albums, query.Get("q"), query.Get("artist"), query.Get("album")))
}

func (s *Server) handleMusicFacets(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	albums, err := service.MusicAlbums(r.Context(), s.media.Browse, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, service.MusicFacetOptions(albums))
}
