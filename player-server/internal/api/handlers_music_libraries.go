package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
)

func (s *Server) handleMusicLibraries(w http.ResponseWriter, r *http.Request) {
	if s.musicLibs == nil {
		writeError(w, http.StatusNotImplemented, "Music libraries are unavailable.")
		return
	}
	libs, err := s.musicLibs.ListForUser(r.Context(), userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	if libs == nil {
		libs = []model.MusicLibrary{}
	}
	writeJSON(w, http.StatusOK, libs)
}

func (s *Server) handleAdminLibraries(w http.ResponseWriter, r *http.Request) {
	if s.musicLibs == nil {
		writeError(w, http.StatusNotImplemented, "Music libraries are unavailable.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		libs, err := s.musicLibs.ListAdmin(r.Context())
		if err != nil {
			handleError(w, err)
			return
		}
		if libs == nil {
			libs = []model.MusicLibrary{}
		}
		writeJSON(w, http.StatusOK, libs)
	case http.MethodPost:
		var body struct {
			Name     string `json:"name"`
			RootPath string `json:"root_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON body.")
			return
		}
		lib, err := s.musicLibs.Create(r.Context(), body.Name, body.RootPath)
		if err != nil {
			handleLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, lib)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAdminLibrary(w http.ResponseWriter, r *http.Request) {
	if s.musicLibs == nil {
		writeError(w, http.StatusNotImplemented, "Music libraries are unavailable.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Invalid library id.")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON body.")
			return
		}
		lib, err := s.musicLibs.Rename(r.Context(), id, body.Name)
		if err != nil {
			handleLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, lib)
	case http.MethodDelete:
		if err := s.musicLibs.Delete(r.Context(), id); err != nil {
			handleLibraryError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAdminLibraryAccess(w http.ResponseWriter, r *http.Request) {
	if s.musicLibs == nil {
		writeError(w, http.StatusNotImplemented, "Music libraries are unavailable.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Invalid library id.")
		return
	}
	var body struct {
		UserID int64      `json:"user_id"`
		Role   model.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	if body.UserID <= 0 {
		writeError(w, http.StatusBadRequest, "user_id is required.")
		return
	}
	switch r.Method {
	case http.MethodPost:
		if err := s.musicLibs.GrantAccess(r.Context(), id, body.UserID, body.Role); err != nil {
			handleLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case http.MethodDelete:
		if err := s.musicLibs.RevokeAccess(r.Context(), id, body.UserID); err != nil {
			handleLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func handleLibraryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrLastLibrary):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrLibraryInUse):
		writeError(w, http.StatusConflict, err.Error())
	default:
		handleError(w, err)
	}
}
