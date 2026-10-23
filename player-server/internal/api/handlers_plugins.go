package api

import (
	"net/http"
	"strings"
)

func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.pluginSvc) {
		return
	}
	items, err := s.pluginSvc.List(r.Context())
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jsonArray(items))
}

func (s *Server) handleGetPlugin(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.pluginSvc) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		badRequest(w, "invalid plugin id")
		return
	}
	info, err := s.pluginSvc.Get(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.pluginSvc) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		badRequest(w, "invalid plugin id")
		return
	}
	info, err := s.pluginSvc.Enable(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleDisablePlugin(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.pluginSvc) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		badRequest(w, "invalid plugin id")
		return
	}
	info, err := s.pluginSvc.Disable(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleInvokePlugin(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.pluginSvc) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		badRequest(w, "invalid plugin id")
		return
	}
	var req struct {
		Capability string `json:"capability"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Capability) == "" {
		badRequest(w, "capability is required")
		return
	}
	res, err := s.pluginSvc.Invoke(r.Context(), id, req.Capability)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
