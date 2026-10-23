package api

import (
	"io"
	"net/http"
	"strings"

	"github.com/snonux/player/internal/service"
)

func (s *Server) handleMusicArtwork(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-cache, must-revalidate")
	kind, key := r.PathValue("kind"), r.PathValue("entity_key")
	canEdit := false
	switch kind {
	case "artist":
		if !requireService(w, s.media.Browse) {
			return
		}
		albums, err := service.MusicAlbums(r.Context(), s.media.Browse, userIDFromContext(r))
		if err != nil {
			handleError(w, err)
			return
		}
		if _, ok := service.MusicEnrichmentIdentity(albums, "artist", key); !ok {
			writeError(w, 404, "Artist is unavailable or you no longer have permission.")
			return
		}
		user, err := s.store.GetUserByID(r.Context(), userIDFromContext(r))
		if err != nil {
			handleError(w, err)
			return
		}
		canEdit = user != nil && user.IsAdmin
	case "playlist":
		if !requireService(w, s.media.Playlists) {
			return
		}
		p, err := s.media.Playlists.Get(r.Context(), userIDFromContext(r), key)
		if err != nil {
			handleError(w, err)
			return
		}
		canEdit = p.CanEdit
	default:
		writeError(w, 400, "Artwork is available for artists and playlists.")
		return
	}
	if r.Method == http.MethodPut || r.Method == http.MethodDelete {
		if !canEdit {
			writeError(w, 403, "You do not have permission to change this artwork.")
			return
		}
		if r.Method == http.MethodDelete {
			if err := s.store.SaveMusicArtwork(r.Context(), kind, key, nil); err != nil {
				handleError(w, err)
				return
			}
		} else {
			r.Body = http.MaxBytesReader(w, r.Body, service.MaxMusicArtworkBytes)
			data, err := io.ReadAll(r.Body)
			if err != nil {
				writeError(w, 400, "Artwork must be at most 5 MB.")
				return
			}
			art, err := service.NormalizeMusicArtwork(data)
			if err != nil {
				handleError(w, err)
				return
			}
			if err := s.store.SaveMusicArtwork(r.Context(), kind, key, art); err != nil {
				handleError(w, err)
				return
			}
		}
	}
	art, err := s.store.GetMusicArtwork(r.Context(), kind, key)
	if err != nil {
		handleError(w, err)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/image") {
		if art == nil {
			writeError(w, 404, "No custom artwork.")
			return
		}
		etag := `"` + art.Revision + `"`
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(art.Data)
		return
	}
	revision := ""
	if art != nil {
		revision = art.Revision
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"has_artwork": art != nil, "can_edit": canEdit, "revision": revision})
}
