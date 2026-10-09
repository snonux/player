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
// credentials the client presented, and whether it is a probe. HEAD delivers
// no content, so it never opens a viewing and never costs a share use.
func shareAccess(r *http.Request) service.ShareAccess {
	return service.ShareAccess{
		Token:       r.PathValue("token"),
		Credentials: shareCredentials(r),
		Probe:       r.Method == http.MethodHead,
	}
}

// shareCredentials returns the viewing credentials presented with r, in the
// order the service tries them: the "view" query parameter (media players,
// which keep no cookies), then the viewing cookie (browsers). Both are
// passed on, so a stale parameter does not shadow a valid cookie. They are
// unverified — the service decides — and must never be logged.
func shareCredentials(r *http.Request) []string {
	var credentials []string
	if credential := r.URL.Query().Get(service.ShareViewParam); credential != "" {
		credentials = append(credentials, credential)
	}
	if cookie, err := r.Cookie(shareViewCookie); err == nil && cookie.Value != "" {
		credentials = append(credentials, cookie.Value)
	}
	return credentials
}

// handOverShareViewing gives the client the credential of a viewing this
// request opened, as a cookie. Nothing is sent when the client already had
// the viewing, so its cookie keeps the original expiry.
//
// The cookie is scoped to the share's path: it is sent with the page and its
// stream/compat/thumbnail/download requests and with nothing else, and one
// share's cookie never reaches another share. HttpOnly keeps the value out of
// document.cookie, which is hygiene rather than secrecy: a script running on
// this origin can still obtain a credential by requesting the share's JSON
// form. SameSite=Lax rather than Strict, because share links are opened
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
