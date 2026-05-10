package api

import (
	"net/http"

	"codeberg.org/snonux/player/internal/model"
)

// ------------------------------------------------------------------
// Admin
// ------------------------------------------------------------------

func (s *Server) handleListTrash(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.adminSvc) {
		return
	}
	items, err := s.adminSvc.ListTrash(r.Context())
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.adminSvc) {
		return
	}
	if err := s.adminSvc.TriggerRescan(r.Context()); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleScanProgress(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.adminSvc) {
		return
	}
	writeJSON(w, http.StatusOK, s.adminSvc.ScanProgress(r.Context()))
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.adminSvc) {
		return
	}
	users, err := s.adminSvc.ListUsers(r.Context())
	if err != nil {
		handleError(w, err)
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
		badRequest(w, "invalid request")
		return
	}
	user, err := s.adminSvc.CreateUser(r.Context(), req.Username, req.Password, req.IsAdmin)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.adminSvc) {
		return
	}
	id := pathID(r, "id")
	if id == 0 {
		badRequest(w, "invalid user id")
		return
	}
	adminUser, _ := r.Context().Value(userCtxKey).(*model.User)
	var callerID int64
	if adminUser != nil {
		callerID = adminUser.ID
	}
	if err := s.adminSvc.DeleteUser(r.Context(), callerID, id); err != nil {
		handleError(w, err)
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
		handleError(w, err)
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
		badRequest(w, "invalid request")
		return
	}
	if err := s.adminSvc.GrantPermission(r.Context(), req.SetID, req.UserID, req.Role); err != nil {
		handleError(w, err)
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
		badRequest(w, "invalid request")
		return
	}
	if err := s.adminSvc.RevokePermission(r.Context(), req.SetID, req.UserID); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
