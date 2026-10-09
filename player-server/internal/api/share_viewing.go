package api

import (
	"errors"
	"net/http"

	"codeberg.org/snonux/player/internal/service"
)

// shareViewCookie is the cookie that carries a share viewing credential for
// browsers. One name serves all shares because the cookie is scoped to the
// share's own path (/s/{token}).
const shareViewCookie = "share_view"

// preparePublicShare sets the headers every public share response carries.
//
// The token in the path and the viewing credential in the query string are
// the only credentials, so nothing may be cached by intermediaries, and the
// URL must not travel to other sites in a Referer header.
func preparePublicShare(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// shareAccess describes the share request r: its token, the viewing
// credential the client presented, and whether it is a probe. HEAD delivers
// no content, so it never opens a viewing and never costs a share use.
func shareAccess(r *http.Request) service.ShareAccess {
	return service.ShareAccess{
		Token:      r.PathValue("token"),
		Credential: shareCredential(r),
		Probe:      r.Method == http.MethodHead,
	}
}

// shareCredential returns the viewing credential presented with r: the
// "view" query parameter (media players, which keep no cookies) or else the
// viewing cookie (browsers). It is returned unverified — the service decides
// — and must never be logged.
func shareCredential(r *http.Request) string {
	if credential := r.URL.Query().Get(service.ShareViewParam); credential != "" {
		return credential
	}
	if cookie, err := r.Cookie(shareViewCookie); err == nil {
		return cookie.Value
	}
	return ""
}

// handOverShareViewing gives the client the credential of a viewing this
// request opened, as a cookie. Nothing is sent when the client already had
// the viewing, so its cookie keeps the original expiry.
//
// The cookie is scoped to the share's path: it is sent with the page and its
// stream/compat/thumbnail/download requests and with nothing else, and one
// share's cookie never reaches another share. HttpOnly keeps it from page
// scripts. SameSite=Lax rather than Strict, because share links are opened
// from other sites (webmail, chat): Strict would withhold the cookie on such
// a navigation and every click on the link would cost another use. The media
// requests themselves are same-origin and unaffected by either setting.
func (s *Server) handOverShareViewing(w http.ResponseWriter, token string, viewing service.ShareViewing) {
	if !viewing.Opened {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     shareViewCookie,
		Value:    viewing.Credential,
		Path:     "/s/" + token,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		// MaxAge for modern browsers, Expires as the legacy fallback; both
		// end with the viewing.
		MaxAge:  int(viewing.ExpiresAt.Sub(s.clk.Now()).Seconds()),
		Expires: viewing.ExpiresAt,
	})
}

// writeShareError maps a share service error to the plain-text answers of
// the public share routes: 410 for a share that expired or has no use left,
// 404 for an unknown (or revoked) share or missing media. Anything else is
// logged and answered generically, because these routes are anonymous and
// internal error text does not belong in their responses.
func (s *Server) writeShareError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrShareExpired):
		http.Error(w, "gone", http.StatusGone)
	case errors.Is(err, service.ErrShareNotFound), errors.Is(err, service.ErrMediaNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		// r.URL.Path only: the query string may hold a viewing credential.
		s.logger.Error("public share request failed", "path", r.URL.Path, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
