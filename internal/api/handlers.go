package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"codeberg.org/snonux/play/internal/model"
	"codeberg.org/snonux/play/internal/repository"
	"codeberg.org/snonux/play/internal/service"
)

type bootstrapRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("encode json", "err", err)
	}
}

func readJSON(r *http.Request, dst interface{}) error {
	if r.Body == nil {
		return fmt.Errorf("missing body")
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst)
}

func pathID(r *http.Request, name string) int64 {
	id, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id
}

func userIDFromContext(r *http.Request) int64 {
	sess, _ := r.Context().Value(sessionCtxKey).(*model.Session)
	if sess == nil {
		return 0
	}
	return sess.UserID
}

func sessionIDFromContext(r *http.Request) string {
	sess, _ := r.Context().Value(sessionCtxKey).(*model.Session)
	if sess == nil {
		return ""
	}
	return sess.ID
}

func stringPtr(s string) *string { return &s }

func floatPtr(f float64) *float64 { return &f }

func intPtr(i int64) *int64 { return &i }

func requireService(w http.ResponseWriter, svc any) bool {
	if svc == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not implemented"})
		return false
	}
	return true
}

// ------------------------------------------------------------------
// Static pages
// ------------------------------------------------------------------

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, filename string) {
	f, err := s.staticFS.Open(filename)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.ServeContent(w, r, filename, stat.ModTime(), f.(io.ReadSeeker))
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	s.serveFile(w, r, "index.html")
}

func (s *Server) serveLogin(w http.ResponseWriter, r *http.Request) {
	s.serveFile(w, r, "login.html")
}

func (s *Server) serveBootstrap(w http.ResponseWriter, r *http.Request) {
	s.serveFile(w, r, "bootstrap.html")
}

// ------------------------------------------------------------------
// Bootstrap & Auth
// ------------------------------------------------------------------

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	var req bootstrapRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password required"})
		return
	}

	ctx := r.Context()
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if count > 0 {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "bootstrap already complete"})
		return
	}

	hash, err := s.hasher.Hash(req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	user := &model.User{Username: req.Username, PasswordHash: hash, IsAdmin: true, CreatedAt: time.Now()}
	id, err := s.store.CreateUser(ctx, user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	user.ID = id

	sessID, err := s.sm.CreateSession(ctx, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	s.setSessionCookie(w, sessID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": user.ID, "username": user.Username, "is_admin": user.IsAdmin})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password required"})
		return
	}

	ctx := r.Context()
	user, err := s.store.GetUserByUsername(ctx, req.Username)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	if err := s.hasher.Compare(user.PasswordHash, req.Password); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	sessID, err := s.sm.CreateSession(ctx, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	s.setSessionCookie(w, sessID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": user.ID, "username": user.Username, "is_admin": user.IsAdmin})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil && cookie.Value != "" {
		_ = s.sm.DeleteSession(r.Context(), cookie.Value)
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.pingStore(r.Context()); err != nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(time.Duration(s.cfg.SessionTimeoutHours) * time.Hour),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

// ------------------------------------------------------------------
// Sets
// ------------------------------------------------------------------

func (s *Server) handleListSets(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	sets, err := s.mediaSvc.ListSets(r.Context(), userIDFromContext(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sets)
}

func (s *Server) handleSetCover(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	setID := pathID(r, "id")
	if setID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid set id"})
		return
	}
	if err := s.mediaSvc.RegenerateSetCover(r.Context(), setID, userIDFromContext(r)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	setID := pathID(r, "id")
	if setID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid set id"})
		return
	}
	_ = r.ParseMultipartForm(int64(s.cfg.MaxUploadSizeMB) << 20)
	file, fh, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing file"})
		return
	}
	defer file.Close()

	media, err := s.mediaSvc.UploadMedia(r.Context(), setID, userIDFromContext(r), fh.Filename, file, fh.Size)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, media)
}

// ------------------------------------------------------------------
// Media
// ------------------------------------------------------------------

// parseMediaListQuery extracts and validates query parameters from the request
// and returns a populated repository.MediaFilter with sensible defaults.
func parseMediaListQuery(q url.Values) repository.MediaFilter {
	filter := repository.MediaFilter{
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
	if v := q.Get("type"); v != "" {
		t := model.MediaType(v)
		filter.Type = &t
	}
	if v := q.Get("favorites"); v != "" {
		if uid, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.Favorites = &uid
		}
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
	if !requireService(w, s.mediaSvc) {
		return
	}
	filter := parseMediaListQuery(r.URL.Query())
	media, err := s.mediaSvc.ListMedia(r.Context(), userIDFromContext(r), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, media)
}

func (s *Server) handleGetMedia(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	detail, err := s.mediaSvc.GetMediaDetail(r.Context(), id, userIDFromContext(r))
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
	if !requireService(w, s.mediaSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	fav, err := s.mediaSvc.ToggleFavorite(r.Context(), userIDFromContext(r), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"favorite": fav})
}

func (s *Server) handleAddTag(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
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
	if err := s.mediaSvc.AssignTag(r.Context(), id, userIDFromContext(r), req.Tag); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRemoveTag(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	id := pathID(r, "id")
	tagName := r.PathValue("tag")
	if id == 0 || tagName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid parameters"})
		return
	}
	if err := s.mediaSvc.RemoveTag(r.Context(), id, userIDFromContext(r), tagName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSoftDelete(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	if err := s.mediaSvc.SoftDeleteMedia(r.Context(), id, userIDFromContext(r)); err != nil {
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
	if !requireService(w, s.mediaSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	if err := s.mediaSvc.RestoreMedia(r.Context(), id, userIDFromContext(r)); err != nil {
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
// File serving helpers
// ------------------------------------------------------------------

func (s *Server) serveFileResult(w http.ResponseWriter, r *http.Request, res *service.FileResult, attachment bool) {
	f, err := os.Open(res.Path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if attachment {
		disp := fmt.Sprintf("attachment; filename=%q", res.FileName)
		w.Header().Set("Content-Disposition", disp)
	}

	http.ServeContent(w, r, res.FileName, stat.ModTime(), f)
}

func (s *Server) fileHandler(fn func(context.Context, int64, int64) (*service.FileResult, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := pathID(r, "id")
		if id == 0 {
			http.Error(w, "invalid media id", http.StatusBadRequest)
			return
		}
		res, err := fn(r.Context(), id, userIDFromContext(r))
		if err != nil {
			if errors.Is(err, service.ErrNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if errors.Is(err, service.ErrForbidden) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if res == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		s.serveFileResult(w, r, res, false)
	}
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	s.fileHandler(s.mediaSvc.StreamMedia)(w, r)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	res, err := s.mediaSvc.DownloadMedia(r.Context(), id, userIDFromContext(r))
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
	if res == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	s.serveFileResult(w, r, res, true)
}

func (s *Server) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	s.fileHandler(s.mediaSvc.GetThumbnail)(w, r)
}

func (s *Server) handleRegenThumbnail(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	if err := s.mediaSvc.RegenerateThumbnail(r.Context(), id, userIDFromContext(r)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ------------------------------------------------------------------
// Share routes
// ------------------------------------------------------------------

func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	expiresAt := time.Now().Add(time.Duration(s.cfg.ShareDefaultExpiryDays) * 24 * time.Hour)
	share, err := s.mediaSvc.CreateShare(r.Context(), userIDFromContext(r), id, expiresAt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, share)
}

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	shares, err := s.mediaSvc.ListShares(r.Context(), id, userIDFromContext(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, shares)
}

func (s *Server) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	token := r.PathValue("token")
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token required"})
		return
	}
	if err := s.mediaSvc.RevokeShare(r.Context(), token, userIDFromContext(r)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSharePage(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	token := r.PathValue("token")
	share, err := s.mediaSvc.ValidateShareToken(r.Context(), token)
	if err != nil || share == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, share)
}

func (s *Server) handleShareStream(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	token := r.PathValue("token")
	res, err := s.mediaSvc.StreamSharedMedia(r.Context(), token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if res == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.serveFileResult(w, r, res, false)
}

// ------------------------------------------------------------------
// Notes
// ------------------------------------------------------------------

func (s *Server) handleGetNote(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	note, err := s.mediaSvc.GetNote(r.Context(), id, userIDFromContext(r))
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
	if !requireService(w, s.mediaSvc) {
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
	if err := s.mediaSvc.UpsertNote(r.Context(), note); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, note)
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.mediaSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media id"})
		return
	}
	if err := s.mediaSvc.DeleteNote(r.Context(), id, userIDFromContext(r)); err != nil {
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
	err := s.progressSvc.UpdateProgress(
		r.Context(),
		sessionIDFromContext(r),
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

// ------------------------------------------------------------------
// Admin
// ------------------------------------------------------------------

func (s *Server) handleListTrash(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.adminSvc) {
		return
	}
	items, err := s.adminSvc.ListTrash(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.adminSvc) {
		return
	}
	if err := s.adminSvc.TriggerRescan(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.adminSvc) {
		return
	}
	users, err := s.adminSvc.ListUsers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.adminSvc) {
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}
	if err := readJSON(r, &req); err != nil || req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	user, err := s.adminSvc.CreateUser(r.Context(), req.Username, req.Password, req.IsAdmin)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.adminSvc) {
		return
	}
	id := pathID(r, "id")
	adminUser, _ := r.Context().Value(userCtxKey).(*model.User)
	if adminUser != nil && adminUser.ID == id {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot delete self"})
		return
	}
	if err := s.adminSvc.DeleteUser(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListPermissions(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.adminSvc) {
		return
	}
	perms, err := s.adminSvc.ListPermissions(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, perms)
}

func (s *Server) handleGrantPermission(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.adminSvc) {
		return
	}
	var req struct {
		SetID  int64      `json:"set_id"`
		UserID int64      `json:"user_id"`
		Role   model.Role `json:"role"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if err := s.adminSvc.GrantPermission(r.Context(), req.SetID, req.UserID, req.Role); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRevokePermission(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.adminSvc) {
		return
	}
	var req struct {
		SetID  int64 `json:"set_id"`
		UserID int64 `json:"user_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if err := s.adminSvc.RevokePermission(r.Context(), req.SetID, req.UserID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
