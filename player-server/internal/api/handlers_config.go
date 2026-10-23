package api

import (
	"net/http"

	"github.com/snonux/player/internal"
)

// podcastSyncVersion is the podcast_sync capability advertised in the
// config (docs/podcast-sync-contract.md §6.1): the server implements
// POST /api/v1/podcasts/sync with subscription and episode registers. A
// client that does not find it runs podcasts standalone for that account.
const podcastSyncVersion = 1

// handleConfig serves the client configuration and capabilities.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	pageSize := internal.DefaultMediaPageSize
	if s.cfg != nil && s.cfg.MediaPageSize > 0 {
		pageSize = s.cfg.MediaPageSize
	}
	body := map[string]any{
		"media_page_size": int64(pageSize),
		"user_id":         userIDFromContext(r),
		"podcast_sync":    podcastSyncVersion,
		"auth":            s.authModeInfo(),
	}
	writeJSON(w, http.StatusOK, body)
}

// authModeInfo describes configured sign-in modes for clients and the admin UI.
// Trusted-source CIDRs are not exposed; only whether proxy auth is enabled and
// the header / logout URL operators configured.
func (s *Server) authModeInfo() map[string]any {
	info := map[string]any{
		"password": true,
		"bearer":   true,
		"proxy":    false,
	}
	if s.cfg == nil || s.cfg.ProxyAuth == nil || !s.cfg.ProxyAuth.Enabled() {
		return info
	}
	info["proxy"] = true
	proxy := map[string]any{
		"enabled":     true,
		"user_header": s.cfg.ProxyAuth.UserHeader,
	}
	if s.cfg.ProxyAuth.LogoutURL != "" {
		proxy["logout_url"] = s.cfg.ProxyAuth.LogoutURL
	}
	info["proxy_auth"] = proxy
	return info
}
