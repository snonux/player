package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"codeberg.org/snonux/player/internal/service"
	"codeberg.org/snonux/player/internal/web"
)

type createShareRequest struct {
	ExpiresAt *time.Time `json:"expires_at"`
	MaxUses   *int       `json:"max_uses"`
}

// ------------------------------------------------------------------
// Share routes
// ------------------------------------------------------------------

func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Share) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	// Use the injected clock so tests can pin "now" and assert deterministic
	// share-expiry semantics (e.g. assert that expiresAt is exactly
	// ShareDefaultExpiryDays * 24h after the mock clock's T).
	now := s.clk.Now()
	expiresAt := now.Add(time.Duration(s.cfg.ShareDefaultExpiryDays) * 24 * time.Hour)
	var req createShareRequest
	if r.Body != nil {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			badRequest(w, "invalid request body")
			return
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			badRequest(w, "invalid request body")
			return
		}
	}
	if req.ExpiresAt != nil {
		expiresAt = *req.ExpiresAt
	}
	if !expiresAt.After(now) || (req.MaxUses != nil && *req.MaxUses < 1) {
		badRequest(w, "expiry must be in the future and max_uses must be positive")
		return
	}
	share, err := s.media.Share.CreateShare(r.Context(), userIDFromContext(r), id, expiresAt, req.MaxUses)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, share)
}

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Share) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	shares, err := s.media.Share.ListShares(r.Context(), id, userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jsonArray(shares))
}

func (s *Server) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Share) {
		return
	}
	token := r.PathValue("token")
	if token == "" {
		badRequest(w, "token required")
		return
	}
	if err := s.media.Share.RevokeShare(r.Context(), token, userIDFromContext(r)); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleSharePage serves the public share page, or the share metadata as
// JSON when the client asks for application/json.
//
// The two forms count uses differently, because of who asks for them:
//
//   - HTML is what link previewers, mail scanners and prefetchers request
//     (chat apps fetch every link they see). It therefore never opens a
//     viewing, never costs a use and never sets the cookie. The page's script
//     opens the viewing itself with POST /s/{token}/view before it touches
//     any media URL (handleShareOpenViewing).
//   - JSON is requested by apps and API clients that are about to play. A GET
//     without a valid viewing credential opens a viewing — one use — and
//     returns the credential in the body, with every URL already carrying
//     it, and as a cookie.
//
// Either form is served only while the share has uses left or the request
// presents a valid credential. HEAD never opens a viewing.
func (s *Server) handleSharePage(w http.ResponseWriter, r *http.Request) {
	preparePublicShare(w)
	if !requireService(w, s.media.Share) {
		return
	}
	wantsJSON := strings.Contains(r.Header.Get("Accept"), "application/json")
	access := shareAccess(r)
	access.Probe = access.Probe || !wantsJSON
	res, err := s.media.Share.GetSharedMedia(r.Context(), access)
	if err != nil {
		s.writeShareError(w, r, err)
		return
	}
	if res == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Vary", "Accept")
	if !wantsJSON {
		s.renderSharePage(w, r, res)
		return
	}
	s.handOverShareViewing(w, access.Token, res.Viewing)
	writeJSON(w, http.StatusOK, res.WithViewCredential())
}

// handleShareOpenViewing opens the share page's viewing: POST /s/{token}/view.
//
// The page calls it once, before it loads any media. Without a valid viewing
// credential it consumes one share use and sets the viewing cookie; with one
// (a reload) it changes nothing. It answers 204, 410 when no use is left,
// 404 for an unknown or revoked share.
//
// POST, because nothing that merely looks at a link sends one: previewers,
// scanners and browser prefetch issue GET or HEAD. That is what keeps a
// single-use link from being spent before its recipient opens it.
func (s *Server) handleShareOpenViewing(w http.ResponseWriter, r *http.Request) {
	preparePublicShare(w)
	if !requireService(w, s.media.Share) {
		return
	}
	access := shareAccess(r)
	res, err := s.media.Share.GetSharedMedia(r.Context(), access)
	if err != nil {
		s.writeShareError(w, r, err)
		return
	}
	if res == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.handOverShareViewing(w, access.Token, res.Viewing)
	w.WriteHeader(http.StatusNoContent)
}

// renderSharePage writes the HTML share page. Its embedded metadata has
// plain URLs without the viewing credential: the browser sends the cookie
// with the media requests, so the credential does not end up in URLs a user
// might copy or in the page source.
func (s *Server) renderSharePage(w http.ResponseWriter, r *http.Request, res *service.GetSharedMediaResult) {
	// Sanitize the media filename before embedding it in the share page to
	// prevent XSS via HTML injection and to cap memory growth from enormous
	// filenames (DoS). SanitizeFileName truncates to MaxFileNameLength runes
	// and HTML-escapes the result; encoding/json also Unicode-escapes </>
	// inside string values, so both layers reinforce each other.
	if res.Media != nil {
		res.Media.FileName = web.SanitizeFileName(res.Media.FileName)
	}

	// Render the HTML view via the dedicated renderer. This keeps the
	// handler focused on transport concerns (status codes, headers) and
	// keeps templating in the internal/web package.
	page, err := s.shareRenderer.Render(res)
	if err != nil {
		s.logger.Error("render share page", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, page.Name, page.ModTime, strings.NewReader(page.HTML))
}

// handleShareThumbnail serves the shared item's thumbnail. It never opens a
// viewing and never costs a use; a viewing credential only keeps it
// reachable once max_uses is reached.
func (s *Server) handleShareThumbnail(w http.ResponseWriter, r *http.Request) {
	preparePublicShare(w)
	if !requireService(w, s.media.Share) {
		return
	}
	access := shareAccess(r)
	fr, err := s.media.Share.GetSharedThumbnail(r.Context(), access.Token, access.Credentials...)
	if err != nil {
		s.writeShareError(w, r, err)
		return
	}
	if fr == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.serveFileResult(w, r, fr, false)
}

// handleShareStream serves the shared original file for playback.
func (s *Server) handleShareStream(w http.ResponseWriter, r *http.Request) {
	s.serveSharedFile(w, r, false)
}

// handleShareDownload serves the shared original file as an attachment.
func (s *Server) handleShareDownload(w http.ResponseWriter, r *http.Request) {
	s.serveSharedFile(w, r, true)
}

// serveSharedFile serves the shared original file.
//
// With a valid viewing credential (cookie or "view" parameter) the request
// costs nothing, however many ranged requests a player makes. Without one, a
// GET opens a viewing of its own — one use — and receives the cookie: links
// pasted straight to /stream and clients that predate viewings keep working
// as they always did, at one use per request unless they return the cookie.
func (s *Server) serveSharedFile(w http.ResponseWriter, r *http.Request, download bool) {
	preparePublicShare(w)
	if !requireService(w, s.media.Share) {
		return
	}
	access := shareAccess(r)
	fr, viewing, err := s.media.Share.StreamSharedMedia(r.Context(), access)
	if err != nil {
		s.writeShareError(w, r, err)
		return
	}
	if fr == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.handOverShareViewing(w, access.Token, viewing)
	s.serveFileResult(w, r, fr, download)
}

func (s *Server) handleMyShares(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Share) {
		return
	}
	shares, err := s.media.Share.ListMyShares(r.Context(), userIDFromContext(r))
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jsonArray(shares))
}
