package api

import (
	"encoding/json"
	"io"
	"net/http"
)

func (s *Server) handleListBookmarks(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Bookmarks) {
		return
	}
	mediaID, err := pathID(r, "id")
	if err != nil || mediaID <= 0 {
		badRequest(w, "invalid media id")
		return
	}
	items, err := s.media.Bookmarks.ListBookmarks(r.Context(), mediaID, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jsonArray(items))
}

func (s *Server) handleCreateBookmark(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Bookmarks) {
		return
	}
	mediaID, err := pathID(r, "id")
	if err != nil || mediaID <= 0 {
		badRequest(w, "invalid media id")
		return
	}
	var body struct {
		PositionSeconds *float64 `json:"position_seconds"`
		Label           string   `json:"label"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.PositionSeconds == nil {
		badRequest(w, "invalid bookmark: position_seconds is required and must be a number")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		badRequest(w, "invalid bookmark JSON")
		return
	}
	item, err := s.media.Bookmarks.CreateBookmark(r.Context(), mediaID, userIDFromContext(r), *body.PositionSeconds, body.Label)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleDeleteBookmark(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Bookmarks) {
		return
	}
	mediaID, err := pathID(r, "id")
	if err != nil || mediaID <= 0 {
		badRequest(w, "invalid media id")
		return
	}
	id, err := pathID(r, "bookmarkId")
	if err != nil || id <= 0 {
		badRequest(w, "invalid bookmark id")
		return
	}
	if err := s.media.Bookmarks.DeleteBookmark(r.Context(), id, mediaID, userIDFromContext(r)); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
