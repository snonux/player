package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/service"
)

func (s *Server) routesRadio() {
	relay := service.NewRadioRelay()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		s.handleBoth(method, "/api/radio", s.requireSession(s.handleRadio))
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		s.handleBoth(method, "/api/radio/{station_id}", s.requireSession(s.handleRadio))
	}
	for _, kind := range []string{"stream", "artwork"} {
		s.handleBoth(http.MethodGet, "/api/radio/{station_id}/"+kind, s.requireSession(func(w http.ResponseWriter, r *http.Request) { s.handleRadioMedia(w, r, relay) }))
	}
}

func (s *Server) radioStore(w http.ResponseWriter) repository.RadioRepo {
	repo, ok := s.store.(repository.RadioRepo)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Internet radio is unavailable.")
		return nil
	}
	return repo
}

func (s *Server) handleRadio(w http.ResponseWriter, r *http.Request) {
	repo := s.radioStore(w)
	if repo == nil {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		items, err := repo.ListRadioStations(r.Context())
		if err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
		return
	}
	user, err := s.store.GetUserByID(r.Context(), userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	if user == nil || !user.IsAdmin {
		writeError(w, http.StatusForbidden, "Only administrators can manage stations.")
		return
	}
	var id int64
	if r.Method != http.MethodPost {
		id, err = strconv.ParseInt(r.PathValue("station_id"), 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "Invalid station.")
			return
		}
		existing, err := repo.GetRadioStation(r.Context(), id)
		if err != nil {
			handleError(w, err)
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "Station removed.")
			return
		}
	}
	if r.Method == http.MethodDelete {
		_, err := repo.DeleteRadioStation(r.Context(), id)
		if err != nil {
			handleError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var item model.RadioStation
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&item); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid station fields.")
		return
	}
	item.ID = id
	if err := service.ValidateRadioStation(&item); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := repo.SaveRadioStation(r.Context(), &item); err != nil {
		handleError(w, err)
		return
	}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	writeJSON(w, status, item)
}

func (s *Server) handleRadioMedia(w http.ResponseWriter, r *http.Request, relay *service.RadioRelay) {
	repo := s.radioStore(w)
	if repo == nil {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("station_id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Invalid station.")
		return
	}
	item, err := repo.GetRadioStation(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "Station removed.")
		return
	}
	if strings.HasSuffix(r.URL.Path, "/artwork") {
		if item.ArtworkURL == "" {
			writeError(w, http.StatusNotFound, "No station artwork.")
			return
		}
		data, err := relay.Artwork(r.Context(), *item)
		if err != nil {
			writeError(w, http.StatusBadGateway, "Station artwork unavailable.")
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=300")
		_, _ = w.Write(data)
		return
	}
	relay.Stream(w, r, *item)
}
