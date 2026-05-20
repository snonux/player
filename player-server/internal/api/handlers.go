package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"codeberg.org/snonux/player/internal/model"
)

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

// writeJSON serialises data to JSON and writes it to w with the given status.
//
// Marshalling happens into an in-memory buffer BEFORE any status or body is
// written to the response. This avoids the previous footgun where the headers
// (and a 200/whatever status) were committed first and then encoding failed
// halfway through, producing a response with a misleading success status and a
// truncated/invalid body. If encoding fails we instead emit a 500 with a small
// JSON error payload so callers always see a consistent error shape.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(data); err != nil {
		slog.Error("encode json", "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal encoding error"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func badRequest(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, message)
}

func notFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "not found")
}

func forbidden(w http.ResponseWriter, message string) {
	writeError(w, http.StatusForbidden, message)
}

// HTTPStatuser is implemented by service errors that know their own HTTP
// status. Sentinels in internal/service implement this so handleError can
// dispatch without an ever-growing switch (OCP): adding a new sentinel only
// requires defining its status alongside the sentinel itself, with no edit
// required here.
type HTTPStatuser interface {
	HTTPStatus() int
}

// handleError dispatches service errors to an HTTP response. If any error in
// the chain implements HTTPStatuser, that status is used together with the
// wrapped error's message (so callers that add context via fmt.Errorf("%w: …")
// keep that context in the body). Unrecognised errors fall back to 500.
func handleError(w http.ResponseWriter, err error) {
	var statuser HTTPStatuser
	if errors.As(err, &statuser) {
		writeError(w, statuser.HTTPStatus(), err.Error())
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, dst interface{}) error {
	if r.Body == nil {
		return errors.New("missing body")
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst)
}

// pathID parses a path variable as an int64. It returns the parsed id together
// with an explicit error so callers can distinguish "missing/malformed" from a
// legitimately zero value and log the underlying ParseInt failure. The error
// is wrapped with the variable name to make server logs actionable.
func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	return id, nil
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
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, filename, stat.ModTime(), rs)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	s.serveFile(w, r, "index.html")
}

func (s *Server) serveLogin(w http.ResponseWriter, r *http.Request) {
	s.serveFile(w, r, "login.html")
}

// serveBootstrap serves bootstrap.html only when no users exist yet.
// Once the first admin account has been created the bootstrap page must no
// longer be reachable — redirecting to /login.html prevents an attacker
// from reaching the form on an already-configured instance.
func (s *Server) serveBootstrap(w http.ResponseWriter, r *http.Request) {
	if s.authSvc != nil {
		count, err := s.authSvc.CountUsers(r.Context())
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if count > 0 {
			// Bootstrap is complete; send browsers to the login page.
			http.Redirect(w, r, "/login.html", http.StatusTemporaryRedirect)
			return
		}
	}
	s.serveFile(w, r, "bootstrap.html")
}

func (s *Server) serveDetach(w http.ResponseWriter, r *http.Request) {
	s.serveFile(w, r, "detach.html")
}

