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
		"media_page_size":          int64(pageSize),
		"user_id":                  userIDFromContext(r),
		"podcast_sync":             podcastSyncVersion,
		"podcast_episode_deletion": true,
		"auth":                     s.authModeInfo(),
		"subsonic":                 s.subsonicStatusInfo(),
	}
	if s.jukeboxSvc != nil && s.jukeboxSvc.Enabled() {
		_, isAdmin := s.jukeboxCaller(r)
		body["jukebox"] = map[string]any{
			"enabled":     true,
			"can_control": isAdmin,
			"name":        "This server's speakers",
			"device_id":   "server-speakers",
		}
	}
	writeJSON(w, http.StatusOK, body)
}

// subsonicStatusInfo is the enable/status surface for external Subsonic clients.
// Credentials are never included.
func (s *Server) subsonicStatusInfo() map[string]any {
	enabled := s.cfg != nil && s.cfg.SubsonicEnabled
	info := map[string]any{
		"enabled":        enabled,
		"base_path":      "/rest",
		"api_version":    "1.16.1",
		"open_subsonic":  true,
		"server_type":    "Player",
		"server_version": internal.Version,
	}
	if enabled {
		info["extensions"] = []string{"apiKeyAuthentication", "formPost"}
		info["auth"] = []string{"password", "enc_password", "apiKey"}
		info["note"] = "Connect DSub/Tempo (or other Subsonic clients) to this server URL. Prefer apiKey (Player API token) or username+password; MD5 token auth is not supported with bcrypt passwords. Revoke access via Settings › API Tokens or by changing the account password."
	}
	return info
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
