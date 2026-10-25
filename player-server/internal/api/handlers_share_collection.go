package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

func (s *Server) handleCreateCollectionShare(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Share) {
		return
	}
	var req struct {
		Title        string     `json:"title"`
		MediaIDs     []int64    `json:"media_ids"`
		ExpiresAt    *time.Time `json:"expires_at"`
		MaxUses      *int       `json:"max_uses"`
		AudioQuality string     `json:"audio_quality"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		badRequest(w, "invalid collection share request")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		badRequest(w, "invalid collection share request")
		return
	}
	expiry := s.clk.Now().Add(time.Duration(s.cfg.ShareDefaultExpiryDays) * 24 * time.Hour)
	if req.ExpiresAt != nil {
		expiry = *req.ExpiresAt
	}
	share, err := s.media.Share.CreateCollectionShare(r.Context(), userIDFromContext(r), req.Title, req.MediaIDs, expiry, req.MaxUses, req.AudioQuality)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, share)
}
