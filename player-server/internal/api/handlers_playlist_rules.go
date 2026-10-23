package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"codeberg.org/snonux/player/internal/model"
)

func (s *Server) handlePlaylistRules(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Playlists) {
		return
	}
	var body struct {
		Rules json.RawMessage `json:"rules"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.Rules == nil {
		writeError(w, 400, "Supply playlist rules or null to disable them.")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, 400, "Supply one rules request.")
		return
	}
	var rules *model.PlaylistRules
	if string(body.Rules) != "null" {
		rules = new(model.PlaylistRules)
		inner := json.NewDecoder(strings.NewReader(string(body.Rules)))
		inner.DisallowUnknownFields()
		if err := inner.Decode(rules); err != nil {
			writeError(w, 400, "Invalid playlist rules.")
			return
		}
	}
	result, err := s.media.Playlists.SetRules(r.Context(), userIDFromContext(r), r.PathValue("playlist_id"), rules)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
