package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

func (s *Server) handleUpdateShare(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Share) {
		return
	}
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	if err := decoder.Decode(&fields); err != nil || len(fields) == 0 {
		badRequest(w, "invalid request body")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		badRequest(w, "invalid request body")
		return
	}
	var description *string
	var expiresAt *time.Time
	for key, value := range fields {
		if string(value) == "null" {
			badRequest(w, "editable fields cannot be null")
			return
		}
		switch key {
		case "description":
			var text string
			if err := json.Unmarshal(value, &text); err != nil {
				badRequest(w, "invalid description")
				return
			}
			description = &text
		case "expires_at":
			var expiry time.Time
			if err := json.Unmarshal(value, &expiry); err != nil {
				badRequest(w, "invalid expiry")
				return
			}
			expiresAt = &expiry
		default:
			badRequest(w, "unknown share field")
			return
		}
	}
	share, err := s.media.Share.UpdateShare(r.Context(), r.PathValue("token"), userIDFromContext(r), description, expiresAt)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, share)
}
