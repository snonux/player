package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handlePlaylists(w http.ResponseWriter, r *http.Request) {
	svc := s.media.Playlists
	if !requireService(w, svc) {
		return
	}
	user, id := userIDFromContext(r), r.PathValue("playlist_id")
	var result any
	var err error
	switch r.Method {
	case http.MethodGet:
		if id == "" {
			result, err = svc.List(r.Context(), user)
		} else {
			result, err = svc.Get(r.Context(), user, id)
		}
	case http.MethodDelete:
		err = svc.Delete(r.Context(), user, id)
	default:
		var body struct {
			Name     string `json:"name"`
			Comment  string `json:"comment"`
			AlbumKey string `json:"album_key"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32768)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writeError(w, 400, "Invalid playlist request.")
			return
		}
		if r.Method == http.MethodPost && id != "" {
			result, err = svc.AddAlbum(r.Context(), user, id, body.AlbumKey)
		} else if r.Method == http.MethodPost {
			result, err = svc.Create(r.Context(), user, body.Name, body.Comment)
		} else {
			result, err = svc.Update(r.Context(), user, id, body.Name, body.Comment)
		}
	}
	if err != nil {
		handleError(w, err)
		return
	}
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	status := http.StatusOK
	if r.Method == http.MethodPost && id == "" {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}
