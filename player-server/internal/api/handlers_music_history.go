package api

import (
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/service"
	"encoding/json"
	"net/http"
	"strconv"
)

func (s *Server) handleMusicHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	repo, ok := s.store.(repository.MusicHistoryRepo)
	if !ok || !requireService(w, s.media.Browse) {
		if !ok {
			writeError(w, 503, "Listening history is unavailable.")
		}
		return
	}
	user := userIDFromContext(r)
	if expected := r.URL.Query().Get("expected_user_id"); expected != "" {
		id, err := strconv.ParseInt(expected, 10, 64)
		if err != nil || id != user {
			writeError(w, http.StatusConflict, "History belongs to another account. Refresh after signing in.")
			return
		}
	}
	switch r.Method {
	case http.MethodDelete:
		if err := repo.ClearMusicHistory(r.Context(), user); err != nil {
			handleError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPut:
		var body struct {
			Recording     *bool `json:"recording"`
			RetentionDays *int  `json:"retention_days"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil || body.Recording == nil || body.RetentionDays == nil {
			badRequest(w, "recording and retention_days required")
			return
		}
		settings := model.MusicHistorySettings{Recording: *body.Recording, RetentionDays: *body.RetentionDays}
		if err := service.ConfigureMusicHistory(r.Context(), repo, user, settings); err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, settings)
	default:
		var cursor [2]int64
		for i, key := range []string{"before_ms", "before_id"} {
			if raw := r.URL.Query().Get(key); raw != "" {
				value, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || value <= 0 {
					badRequest(w, "Invalid history cursor.")
					return
				}
				cursor[i] = value
			}
		}
		if (cursor[0] == 0) != (cursor[1] == 0) {
			badRequest(w, "Both history cursor fields required.")
			return
		}
		result, err := service.MusicHistory(r.Context(), repo, s.media.Browse, user, cursor[0], cursor[1])
		if err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
