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
			Visibility *string `json:"visibility"`
			OwnerID    *int64  `json:"owner_id"`
			Name       string  `json:"name"`
			Comment    string  `json:"comment"`
			AlbumKey   string  `json:"album_key"`
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
			if body.OwnerID != nil {
				writeError(w, 400, "Ownership can be changed after creating a playlist.")
				return
			}
			result, err = svc.CreateDetails(r.Context(), user, body.Name, body.Comment, body.Visibility)
		} else {
			result, err = svc.UpdateDetails(r.Context(), user, id, body.Name, body.Comment, body.Visibility, body.OwnerID)
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

func (s *Server) handlePlaylistSongs(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Playlists) {
		return
	}
	var body struct {
		Action   string  `json:"action"`
		EntryID  string  `json:"entry_id"`
		BeforeID string  `json:"before_entry_id"`
		MediaIDs []int64 `json:"media_ids"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, 400, "Invalid playlist song request.")
		return
	}
	result, err := s.media.Playlists.EditSongs(r.Context(), userIDFromContext(r), r.PathValue("playlist_id"), body.Action, body.EntryID, body.BeforeID, body.MediaIDs)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
