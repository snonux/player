package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/snonux/player/internal/auth"
	"github.com/snonux/player/internal/service"
)

// castTicketTTL bounds how long a cast URL works. It must outlast a long
// film or audiobook chapter including pauses, because the cast device keeps
// requesting byte ranges with the same URL for as long as it plays.
const castTicketTTL = 12 * time.Hour

// castTicketResponse is the answer of POST /api/media/{id}/cast. The sender
// builds the receiver's URLs from its own server base URL and the ticket:
// /cast/{ticket}/stream, /cast/{ticket}/compat and /cast/{ticket}/thumbnail.
type castTicketResponse struct {
	Ticket            string    `json:"ticket"`
	ExpiresAt         time.Time `json:"expires_at"`
	StreamContentType string    `json:"stream_content_type"`
	CompatContentType string    `json:"compat_content_type"`
}

// routesCast wires the cast ticket API and the public routes a cast device
// fetches media from. The /cast/ routes carry their credential in the path,
// like share links, so they are public to the session middleware.
func (s *Server) routesCast() {
	s.handleBoth(http.MethodPost, "/api/media/{id}/cast", s.requireSession(s.handleCreateCastTicket))
	s.castRoute("/cast/{ticket}/stream", s.handleCastStream)
	s.castRoute("/cast/{ticket}/compat", s.handleCastCompatStream)
	s.castRoute("/cast/{ticket}/thumbnail", s.handleCastThumbnail)
	s.mux.HandleFunc("/cast/", func(w http.ResponseWriter, _ *http.Request) {
		prepareCastResponse(w)
		http.Error(w, "not found", http.StatusNotFound)
	})
	s.mw.RegisterPublicPrefix("/cast/")
}

// castRoute registers a GET (and HEAD) cast route, answers CORS preflights
// and refuses every other method.
func (s *Server) castRoute(path string, handler http.HandlerFunc) {
	s.mux.HandleFunc(http.MethodGet+" "+path, handler)
	s.mux.HandleFunc(http.MethodOptions+" "+path, func(w http.ResponseWriter, _ *http.Request) {
		prepareCastResponse(w)
		w.WriteHeader(http.StatusNoContent)
	})
	s.mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
		prepareCastResponse(w)
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
}

// prepareCastResponse sets the headers of every /cast/ response. The ticket
// is the only credential, so nothing is cached or leaked as a referrer.
// Receivers that load media with fetch/XHR (adaptive players, some Cast
// receiver builds) need CORS; without a cookie involved, any origin is fine.
func prepareCastResponse(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Range")
	h.Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges, Content-Type")
}

// handleCreateCastTicket issues a ticket for a media item the signed-in user
// may stream, with the content types the receiver must be told.
func (s *Server) handleCreateCastTicket(w http.ResponseWriter, r *http.Request) {
	if !requireService(w, s.media.Browse) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil || id == 0 {
		badRequest(w, "invalid media id")
		return
	}
	userID := userIDFromContext(r)
	res, err := s.media.Browse.StreamMedia(r.Context(), id, userID)
	if err != nil {
		writeFileError(w, err)
		return
	}
	if res == nil {
		notFound(w)
		return
	}
	stream, err := s.streamer.Open(r.Context(), res, false)
	if err != nil {
		writeFileError(w, err)
		return
	}
	streamType := stream.ContentType
	_ = stream.File.Close()

	ticket, expires := s.castTickets.Issue(userID, id, castTicketTTL)
	writeJSON(w, http.StatusOK, castTicketResponse{
		Ticket:            ticket,
		ExpiresAt:         expires,
		StreamContentType: streamType,
		CompatContentType: compatContentType(streamType),
	})
}

// compatContentType is the type of the compatibility rendition: AAC in MP4
// for audio, H.264/AAC MP4 for video (see transcode.Kind).
func compatContentType(streamType string) string {
	if strings.HasPrefix(streamType, "audio/") {
		return "audio/mp4"
	}
	return "video/mp4"
}

// castGrant verifies the request's ticket; on failure it has answered 404,
// the same as for a ticket that names media that does not exist.
func (s *Server) castGrant(w http.ResponseWriter, r *http.Request) (auth.CastTicket, bool) {
	prepareCastResponse(w)
	grant, err := s.castTickets.Verify(r.PathValue("ticket"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return auth.CastTicket{}, false
	}
	return grant, true
}

func (s *Server) handleCastStream(w http.ResponseWriter, r *http.Request) {
	grant, ok := s.castGrant(w, r)
	if !ok || !requireService(w, s.media.Browse) {
		return
	}
	res, err := s.media.Browse.StreamMedia(r.Context(), grant.MediaID, grant.UserID)
	if err != nil || res == nil {
		writeFileError(w, err)
		return
	}
	s.serveFileResult(w, r, res, false)
}

func (s *Server) handleCastCompatStream(w http.ResponseWriter, r *http.Request) {
	grant, ok := s.castGrant(w, r)
	if !ok || !requireService(w, s.media.Compat) {
		return
	}
	rendition, err := s.media.Compat.CompatStream(r.Context(), grant.MediaID, grant.UserID)
	if err != nil {
		s.writeCompatError(w, r, err)
		return
	}
	f, ok := s.openRendition(w, rendition)
	if !ok {
		return
	}
	defer func() { _ = f.Close() }()
	s.serveRendition(w, r, rendition, f)
}

func (s *Server) handleCastThumbnail(w http.ResponseWriter, r *http.Request) {
	grant, ok := s.castGrant(w, r)
	if !ok || !requireService(w, s.media.Browse) {
		return
	}
	res, err := s.media.Browse.GetThumbnail(r.Context(), grant.MediaID, grant.UserID)
	if err != nil || res == nil {
		writeFileError(w, err)
		return
	}
	s.serveFileResult(w, r, res, false)
}

// writeFileError answers a failed media lookup. Access errors answer 404
// like a missing item: a ticket for media the user lost access to must not
// tell the cast device more than that it is gone.
func writeFileError(w http.ResponseWriter, err error) {
	if err == nil || errors.Is(err, service.ErrNotFound) || errors.Is(err, service.ErrForbidden) {
		notFound(w)
		return
	}
	handleError(w, err)
}
