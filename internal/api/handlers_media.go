package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/service"
)

// ------------------------------------------------------------------
// Sets
// ------------------------------------------------------------------

func (s *Server) handleListSets(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.browseSvc) {
		return
	}
	sets, err := s.browseSvc.ListSets(r.Context(), userIDFromContext(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sets)
}

func (s *Server) handleGetSetCover(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.browseSvc) {
		return
	}
	setID := pathID(r, "id")
	if setID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid set id"})
		return
	}
	folder := r.URL.Query().Get("folder")
	fr, err := s.browseSvc.GetSetCover(r.Context(), setID, folder, userIDFromContext(r))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, fr.Path)
}

func (s *Server) handlePostSetCover(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.browseSvc) {
		return
	}
	setID := pathID(r, "id")
	if setID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid set id"})
		return
	}
	folder := r.URL.Query().Get("folder")
	if err := s.browseSvc.RegenerateSetCover(r.Context(), setID, folder, userIDFromContext(r)); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleBrowseSet(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.browseSvc) {
		return
	}
	setID := pathID(r, "id")
	if setID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid set id"})
		return
	}
	parent := r.URL.Query().Get("parent")
	result, err := s.browseSvc.BrowseSet(r.Context(), setID, userIDFromContext(r), parent)
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.writeSvc) {
		return
	}
	setID := pathID(r, "id")
	if setID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid set id"})
		return
	}

	maxBytes := int64(s.cfg.MaxUploadSizeMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
		return
	}

	file, fh, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing file"})
		return
	}
	defer file.Close()

	media, err := s.writeSvc.UploadMedia(r.Context(), setID, userIDFromContext(r), fh.Filename, file, fh.Size)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		if errors.Is(err, service.ErrUnsupportedExtension) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, media)
}

// ------------------------------------------------------------------
// Media
// ------------------------------------------------------------------

// parseMediaListQuery extracts and validates query parameters from the request
// and returns a populated service.MediaQueryFilter with sensible defaults.
func parseMediaListQuery(q url.Values) service.MediaQueryFilter {
	filter := service.MediaQueryFilter{
		Search: q.Get("search"),
		Sort:   q.Get("sort"),
		Limit:  100,
		Offset: 0,
	}
	if v := q.Get("set_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.SetID = &id
		}
	}
	if v := q.Get("set_ids"); v != "" {
		parts := strings.Split(v, ",")
		for _, p := range parts {
			if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
				filter.SetIDs = append(filter.SetIDs, id)
			}
		}
	}
	if v := q.Get("type"); v != "" {
		t := model.MediaType(v)
		filter.Type = &t
	}
	if v := q.Get("favorites"); v == "true" || v == "1" {
		filter.Favorites = true
	}
	if v := q.Get("tags"); v != "" {
		filter.Tags = strings.Split(v, ",")
	}
	if v := q.Get("min_duration"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			filter.MinDuration = &f
		}
	}
	if v := q.Get("max_duration"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			filter.MaxDuration = &f
		}
	}
	if v := q.Get("filesize_min"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.MinFileSize = &n
		}
	}
	if v := q.Get("filesize_max"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.MaxFileSize = &n
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			filter.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			filter.Offset = n
		}
	}
	return filter
}

func (s *Server) handleListMedia(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.browseSvc) {
		return
	}
	path := r.URL.Path
	q := r.URL.Query()
	setID := q.Get("set_id")
	setIDs := q.Get("set_ids")
	search := q.Get("search")
	typ := q.Get("type")
	fav := q.Get("favorites")
	minDur := q.Get("min_duration")
	maxDur := q.Get("max_duration")
	start := time.Now()
	filter := parseMediaListQuery(q)
	media, err := s.browseSvc.ListMedia(r.Context(), userIDFromContext(r), filter)
	dur := time.Since(start)
	if err != nil {
		s.logger.Error("api list media failed", "path", path, "set_id", setID, "set_ids", setIDs, "search", search, "type", typ, "favorites", fav, "min_duration", minDur, "max_duration", maxDur, "duration", dur, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logger.Info("api list media", "path", path, "set_id", setID, "set_ids", setIDs, "search", search, "type", typ, "favorites", fav, "min_duration", minDur, "max_duration", maxDur, "returned", len(media), "duration", dur)
	writeJSON(w, http.StatusOK, media)
}

func (s *Server) handleGetMedia(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.browseSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	detail, err := s.browseSvc.GetMediaDetail(r.Context(), id, userIDFromContext(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if detail == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleFavorite(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.favSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	fav, err := s.favSvc.ToggleFavorite(r.Context(), userIDFromContext(r), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"favorite": fav})
}

func (s *Server) handleAddTag(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.tagSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	var req struct {
		Tag string `json:"tag"`
	}
	if err := readJSON(r, &req); err != nil || req.Tag == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tag required"})
		return
	}
	if err := s.tagSvc.AssignTag(r.Context(), id, userIDFromContext(r), req.Tag); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRemoveTag(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.tagSvc) {
		return
	}
	id := pathID(r, "id")
	tagName := r.PathValue("tag")
	if id == 0 || tagName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid parameters"})
		return
	}
	if err := s.tagSvc.RemoveTag(r.Context(), id, userIDFromContext(r), tagName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSoftDelete(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.writeSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	if err := s.writeSvc.SoftDeleteMedia(r.Context(), id, userIDFromContext(r)); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.writeSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	if err := s.writeSvc.RestoreMedia(r.Context(), id, userIDFromContext(r)); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ------------------------------------------------------------------
// Notes
// ------------------------------------------------------------------

func (s *Server) handleGetNote(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.noteSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	note, err := s.noteSvc.GetNote(r.Context(), id, userIDFromContext(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if note == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, note)
}

func (s *Server) handleUpsertNote(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.noteSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	note := &model.Note{MediaID: id, UserID: userIDFromContext(r), Content: req.Content}
	if err := s.noteSvc.UpsertNote(r.Context(), note); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, note)
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.noteSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	if err := s.noteSvc.DeleteNote(r.Context(), id, userIDFromContext(r)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ------------------------------------------------------------------
// Progress
// ------------------------------------------------------------------

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.progressSvc) {
		return
	}
	var req struct {
		MediaID  int64   `json:"media_id"`
		Position float64 `json:"position_seconds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.MediaID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "media_id required"})
		return
	}
	sessionID := sessionIDFromContext(r)
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session required"})
		return
	}
	err := s.progressSvc.UpdateProgress(
		r.Context(),
		sessionID,
		userIDFromContext(r),
		req.MediaID,
		req.Position,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
