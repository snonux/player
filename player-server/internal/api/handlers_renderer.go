package api

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"codeberg.org/snonux/player/internal/renderer"
)

type rendererCommand struct {
	SessionID string  `json:"session_id"`
	Revision  int64   `json:"revision"`
	DeviceID  string  `json:"device_id"`
	Action    string  `json:"action"`
	MediaID   int64   `json:"media_id"`
	Position  float64 `json:"position"`
}

// routesRenderer keeps LAN discovery and control behind normal authentication.
func (s *Server) routesRenderer() {
	manager := renderer.New()
	// Serialize the revision check with the complete SetURI/Seek/Play SOAP
	// sequence and Stop; a newer Stop cannot be overtaken by an older Play.
	var control sync.Mutex
	s.handleBoth(http.MethodGet, "/api/renderers", s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		devices, err := manager.Discover(r.Context())
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Cannot search the server network for speakers"})
			return
		}
		writeJSON(w, http.StatusOK, devices)
	}))
	s.handleBoth(http.MethodPost, "/api/renderers/control", s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		control.Lock()
		defer control.Unlock()
		s.handleRendererControl(manager, w, r)
	}))
}

func (s *Server) handleRendererControl(manager *renderer.Manager, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var command rendererCommand
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&command); err != nil {
		badRequest(w, "invalid renderer command")
		return
	}
	user := userIDFromContext(r)
	if raw, err := hex.DecodeString(command.SessionID); err != nil || len(raw) != 16 || command.Revision < 1 || command.Revision > 9007199254740991 {
		badRequest(w, "invalid renderer session")
		return
	}
	if command.Position < 0 || command.Position > 7*24*3600 {
		badRequest(w, "invalid position")
		return
	}
	if command.Action == "load" {
		if !requireService(w, s.media.Browse) {
			return
		}
		media, err := s.media.Browse.StreamMedia(r.Context(), command.MediaID, user)
		if err != nil || media == nil {
			writeFileError(w, err)
			return
		}
		stream, err := s.streamer.Open(r.Context(), media, false)
		if err != nil {
			writeFileError(w, err)
			return
		}
		contentType := stream.ContentType
		_ = stream.File.Close()
		if !strings.HasPrefix(contentType, "audio/") {
			badRequest(w, "Choose an audio track for this speaker")
			return
		}
		if err = manager.Command(command.DeviceID, user, command.SessionID, command.Revision, "load"); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		ticket, _ := s.castTickets.Issue(user, command.MediaID, castTicketTTL)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		uri := scheme + "://" + r.Host + "/cast/" + ticket + "/stream"
		// Ticket-scoped media URLs are the only URLs sent to LAN devices. In
		// particular no cookie, bearer token, client URL or proxy header is forwarded.
		metadata := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"><item id="0" parentID="0" restricted="1"><dc:title>` + rendererXML(media.FileName) + `</dc:title><upnp:class>object.item.audioItem.musicTrack</upnp:class><res protocolInfo="http-get:*:` + rendererXML(contentType) + `:*">` + rendererXML(uri) + `</res></item></DIDL-Lite>`
		_, err = manager.Action(r.Context(), command.DeviceID, user, "SetAVTransportURI", map[string]string{"CurrentURI": uri, "CurrentURIMetaData": metadata})
		if err == nil && command.Position > 0 {
			_, err = manager.Action(r.Context(), command.DeviceID, user, "Seek", map[string]string{"Unit": "REL_TIME", "Target": rendererTime(command.Position)})
		}
		if err == nil {
			_, err = manager.Action(r.Context(), command.DeviceID, user, "Play", map[string]string{"Speed": "1"})
		}
		if err != nil {
			_, _ = manager.Action(r.Context(), command.DeviceID, user, "Stop", nil)
			manager.Release(command.DeviceID, user)
			rendererError(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"state": "PLAYING", "duration": media.Duration})
		return
	}
	if err := manager.Command(command.DeviceID, user, command.SessionID, command.Revision, command.Action); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	var action string
	var args map[string]string
	switch command.Action {
	case "play":
		action = "Play"
		args = map[string]string{"Speed": "1"}
	case "pause":
		action = "Pause"
	case "stop":
		action = "Stop"
	case "seek":
		action = "Seek"
		args = map[string]string{"Unit": "REL_TIME", "Target": rendererTime(command.Position)}
	case "status":
		position, err := manager.Action(r.Context(), command.DeviceID, user, "GetPositionInfo", nil)
		if err != nil {
			rendererError(w)
			return
		}
		state, err := manager.Action(r.Context(), command.DeviceID, user, "GetTransportInfo", nil)
		if err != nil {
			rendererError(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"state": state["CurrentTransportState"], "position": rendererSeconds(position["RelTime"]), "duration": rendererSeconds(position["TrackDuration"])})
		return
	default:
		badRequest(w, "unknown renderer action")
		return
	}
	_, err := manager.Action(r.Context(), command.DeviceID, user, action, args)
	if command.Action == "stop" {
		manager.Release(command.DeviceID, user)
	}
	if err != nil {
		rendererError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func rendererError(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Speaker unavailable or refused playback. Check its network and format, then reconnect or return to this device."})
}
func rendererTime(seconds float64) string {
	d := int64(seconds)
	return fmt.Sprintf("%02d:%02d:%02d", d/3600, (d/60)%60, d%60)
}
func rendererSeconds(value string) float64 {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0
	}
	d, err := time.ParseDuration(parts[0] + "h" + parts[1] + "m" + parts[2] + "s")
	if err != nil || d < 0 {
		return 0
	}
	return d.Seconds()
}

func rendererXML(value string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}
