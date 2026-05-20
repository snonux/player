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

const multipartFormMemoryLimit = 32 << 20

// ------------------------------------------------------------------
// Sets
// ------------------------------------------------------------------

func (s *Server) handleListSets(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	sets, err := s.media.Browse.ListSets(r.Context(), userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sets)
}

func (s *Server) handleGetSetCover(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	setID, err := pathID(r, "id")
	if err != nil || setID == 0 {
		badRequest(w, "invalid set id")
		return
	}
	folder := r.URL.Query().Get("folder")
	fr, err := s.media.Browse.GetSetCover(r.Context(), setID, folder, userIDFromContext(r))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			notFound(w)
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w, "forbidden")
			return
		}
		handleError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, fr.Path)
}

func (s *Server) handlePostSetCover(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Write) {
		return
	}
	setID, err := pathID(r, "id")
	if err != nil || setID == 0 {
		badRequest(w, "invalid set id")
		return
	}
	folder := r.URL.Query().Get("folder")
	if err := s.media.Write.RegenerateSetCover(r.Context(), setID, folder, userIDFromContext(r)); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			notFound(w)
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w, "forbidden")
			return
		}
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleBrowseSet(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	setID, err := pathID(r, "id")
	if err != nil || setID == 0 {
		badRequest(w, "invalid set id")
		return
	}
	parent := r.URL.Query().Get("parent")
	result, err := s.media.Browse.BrowseSet(r.Context(), setID, userIDFromContext(r), parent)
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w, "forbidden")
			return
		}
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Write) {
		return
	}
	setID, err := pathID(r, "id")
	if err != nil || setID == 0 {
		badRequest(w, "invalid set id")
		return
	}

	maxBytes := int64(s.cfg.MaxUploadSizeMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(multipartFormMemoryLimit); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file too large"})
			return
		}
		badRequest(w, "invalid multipart form")
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, fh, err := r.FormFile("file")
	if err != nil {
		badRequest(w, "missing file")
		return
	}
	defer file.Close()

	media, err := s.media.Write.UploadMedia(r.Context(), setID, userIDFromContext(r), fh.Filename, file, fh.Size)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			notFound(w)
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w, "forbidden")
			return
		}
		if errors.Is(err, service.ErrUnsupportedExtension) {
			badRequest(w, err.Error())
			return
		}
		handleError(w, err)
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
	if !requireService(w, s.media.Browse) {
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
	media, err := s.media.Browse.ListMedia(r.Context(), userIDFromContext(r), filter)
	dur := time.Since(start)
	if err != nil {
		s.logger.Error("api list media failed", "path", path, "set_id", setID, "set_ids", setIDs, "search", search, "type", typ, "favorites", fav, "min_duration", minDur, "max_duration", maxDur, "duration", dur, "err", err)
		handleError(w, err)
		return
	}
	s.logger.Info("api list media", "path", path, "set_id", setID, "set_ids", setIDs, "search", search, "type", typ, "favorites", fav, "min_duration", minDur, "max_duration", maxDur, "returned", len(media), "duration", dur)
	writeJSON(w, http.StatusOK, media)
}

func (s *Server) handleGetMedia(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	detail, err := s.media.Browse.GetMediaDetail(r.Context(), id, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	if detail == nil {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleFavorite(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Favorite) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	fav, err := s.media.Favorite.ToggleFavorite(r.Context(), userIDFromContext(r), id)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"favorite": fav})
}

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Tag) {
		return
	}
	tags, err := s.media.Tag.ListTags(r.Context(), userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) handleAddTag(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Tag) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	var req struct {
		Tag string `json:"tag"`
	}
	if err := readJSON(r, &req); err != nil || req.Tag == "" {
		badRequest(w, "tag required")
		return
	}
	if err := s.media.Tag.AssignTag(r.Context(), id, userIDFromContext(r), req.Tag); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRemoveTag(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Tag) {
		return
	}
	id, err := pathID(r, "id")
	tagName := r.PathValue("tag")
	if err != nil || id == 0 || tagName == "" {
		badRequest(w, "invalid parameters")
		return
	}
	if err := s.media.Tag.RemoveTag(r.Context(), id, userIDFromContext(r), tagName); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSoftDelete(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Write) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	if err := s.media.Write.SoftDeleteMedia(r.Context(), id, userIDFromContext(r)); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			notFound(w)
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w, "forbidden")
			return
		}
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Write) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	if err := s.media.Write.RestoreMedia(r.Context(), id, userIDFromContext(r)); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			notFound(w)
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			forbidden(w, "forbidden")
			return
		}
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ------------------------------------------------------------------
// Playback hints
// ------------------------------------------------------------------

// handlePlaybackHints returns codec/container metadata for a media item so that
// the client can decide whether to play natively or request a future transcoded
// variant. It performs no actual transcoding — only a DB lookup.
func (s *Server) handlePlaybackHints(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.PlaybackHints) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	hint, err := s.media.PlaybackHints.GetPlaybackHint(r.Context(), id, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hint)
}

// ------------------------------------------------------------------
// Notes
// ------------------------------------------------------------------

func (s *Server) handleGetNote(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Note) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	note, err := s.media.Note.GetNote(r.Context(), id, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	if note == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, note)
}

func (s *Server) handleUpsertNote(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Note) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil {
		badRequest(w, "invalid body")
		return
	}
	note := &model.Note{MediaID: id, UserID: userIDFromContext(r), Content: req.Content}
	if err := s.media.Note.UpsertNote(r.Context(), note); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, note)
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Note) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	if err := s.media.Note.DeleteNote(r.Context(), id, userIDFromContext(r)); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
