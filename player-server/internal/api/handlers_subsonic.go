package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/snonux/player/internal"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
	"github.com/snonux/player/internal/subsonic"
)

// handleSubsonic serves /rest/{action} and /rest/{action}.view.
// Subsonic clients authenticate with their own parameters; these routes are
// public to the session middleware and enforce auth inside the handler.
func (s *Server) handleSubsonic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	action := subsonicAction(r.PathValue("action"))
	version := internal.Version
	if s.cfg == nil || !s.cfg.SubsonicEnabled {
		_ = subsonic.Write(w, r, subsonic.Fail(version, subsonic.ErrGeneric, "Subsonic compatibility is disabled"))
		return
	}
	if action == "getOpenSubsonicExtensions" {
		resp := subsonic.OK(version)
		resp.OpenSubsonicExtensions = subsonic.SupportedExtensions()
		_ = subsonic.Write(w, r, resp)
		return
	}
	user, fail := s.authenticateSubsonic(r)
	if fail != nil {
		_ = subsonic.Write(w, r, fail)
		return
	}
	if s.subsonicSvc == nil || s.media.Browse == nil {
		_ = subsonic.Write(w, r, subsonic.Fail(version, subsonic.ErrGeneric, "Subsonic catalog is unavailable"))
		return
	}
	resp, err := s.dispatchSubsonic(r, action, user)
	if err != nil {
		s.logger.Error("subsonic handler", "action", action, "err", err)
		_ = subsonic.Write(w, r, subsonic.Fail(version, subsonic.ErrGeneric, "Internal error"))
		return
	}
	_ = subsonic.Write(w, r, resp)
}

func (s *Server) authenticateSubsonic(r *http.Request) (*model.User, *subsonic.Response) {
	version := internal.Version
	creds := subsonic.ParseCredentials(r)
	if creds.MissingRequired() {
		return nil, subsonic.Fail(version, subsonic.ErrMissingParameter, "Missing required client or version parameter")
	}
	if creds.UsesTokenAuth() {
		return nil, subsonic.Fail(version, subsonic.ErrTokenAuthUnsupported,
			"MD5 token authentication is not supported; use password (p) or apiKey")
	}
	if creds.UsesAPIKey() {
		return s.userFromAPIKey(r, creds.APIKey)
	}
	if !creds.HasPasswordAuth() {
		return nil, subsonic.Fail(version, subsonic.ErrMissingParameter, "Missing username/password or apiKey")
	}
	if s.authSvc == nil {
		return nil, subsonic.Fail(version, subsonic.ErrGeneric, "Authentication is unavailable")
	}
	user, err := s.authSvc.VerifyPassword(r.Context(), creds.Username, creds.Password)
	if err == nil && user != nil {
		return user, nil
	}
	// OpenSubsonic: accept an API token in place of the account password.
	tokenUser, tokenFail := s.userFromAPIKey(r, creds.Password)
	if tokenFail == nil && tokenUser != nil && strings.EqualFold(tokenUser.Username, creds.Username) {
		return tokenUser, nil
	}
	return nil, subsonic.Fail(version, subsonic.ErrBadCredentials, "Wrong username or password")
}

func (s *Server) userFromAPIKey(r *http.Request, apiKey string) (*model.User, *subsonic.Response) {
	version := internal.Version
	if s.authSvc == nil {
		return nil, subsonic.Fail(version, subsonic.ErrGeneric, "Authentication is unavailable")
	}
	sess, err := s.authSvc.AuthenticateBearer(r.Context(), apiKey)
	if err != nil || sess == nil {
		if errors.Is(err, service.ErrInvalidCredentials) || sess == nil {
			return nil, subsonic.Fail(version, subsonic.ErrBadCredentials, "Wrong username or password")
		}
		return nil, subsonic.Fail(version, subsonic.ErrGeneric, "Authentication failed")
	}
	user, err := s.authSvc.GetUserByID(r.Context(), sess.UserID)
	if err != nil || user == nil {
		return nil, subsonic.Fail(version, subsonic.ErrBadCredentials, "Wrong username or password")
	}
	return user, nil
}

func (s *Server) dispatchSubsonic(r *http.Request, action string, user *model.User) (*subsonic.Response, error) {
	version := internal.Version
	folderID := strings.TrimSpace(firstForm(r, "musicFolderId"))
	switch action {
	case "ping":
		return subsonic.OK(version), nil
	case "getLicense":
		resp := subsonic.OK(version)
		resp.License = &subsonic.License{Valid: true}
		return resp, nil
	case "getMusicFolders":
		folders, err := s.subsonicSvc.MusicFolders(r.Context(), user.ID)
		if err != nil {
			return nil, err
		}
		resp := subsonic.OK(version)
		resp.MusicFolders = &subsonic.MusicFolders{Folder: folders}
		return resp, nil
	case "getIndexes":
		index, err := s.subsonicSvc.ArtistIndex(r.Context(), user.ID, folderID)
		if err != nil {
			return nil, err
		}
		resp := subsonic.OK(version)
		resp.Indexes = &subsonic.Indexes{LastModified: 0, IgnoredArticles: "The El La Los Las Le Les", Index: index}
		return resp, nil
	case "getArtists":
		index, err := s.subsonicSvc.ArtistIndex(r.Context(), user.ID, folderID)
		if err != nil {
			return nil, err
		}
		resp := subsonic.OK(version)
		resp.Artists = &subsonic.Artists{IgnoredArticles: "The El La Los Las Le Les", Index: index}
		return resp, nil
	case "getArtist":
		id := strings.TrimSpace(firstForm(r, "id"))
		if id == "" {
			return subsonic.Fail(version, subsonic.ErrMissingParameter, "Missing id"), nil
		}
		artist, err := s.subsonicSvc.ArtistDetail(r.Context(), user.ID, id, folderID)
		if err != nil {
			return nil, err
		}
		if artist == nil {
			return subsonic.Fail(version, subsonic.ErrDataNotFound, "Artist not found"), nil
		}
		resp := subsonic.OK(version)
		resp.Artist = artist
		return resp, nil
	case "getAlbum":
		id := strings.TrimSpace(firstForm(r, "id"))
		if id == "" {
			return subsonic.Fail(version, subsonic.ErrMissingParameter, "Missing id"), nil
		}
		album, err := s.subsonicSvc.AlbumDetail(r.Context(), user.ID, id, folderID)
		if err != nil {
			return nil, err
		}
		if album == nil {
			return subsonic.Fail(version, subsonic.ErrDataNotFound, "Album not found"), nil
		}
		resp := subsonic.OK(version)
		resp.Album = album
		return resp, nil
	case "getSong":
		id := strings.TrimSpace(firstForm(r, "id"))
		if id == "" {
			return subsonic.Fail(version, subsonic.ErrMissingParameter, "Missing id"), nil
		}
		song, err := s.subsonicSvc.SongDetail(r.Context(), user.ID, id, folderID)
		if err != nil {
			return nil, err
		}
		if song == nil {
			return subsonic.Fail(version, subsonic.ErrDataNotFound, "Song not found"), nil
		}
		resp := subsonic.OK(version)
		resp.Song = song
		return resp, nil
	case "search2", "search3":
		return s.subsonicSearch(r, user, folderID, action == "search3")
	default:
		return subsonic.Fail(version, subsonic.ErrGeneric, "Action not implemented in this Player version"), nil
	}
}

func (s *Server) subsonicSearch(r *http.Request, user *model.User, folderID string, id3 bool) (*subsonic.Response, error) {
	version := internal.Version
	query := firstForm(r, "query")
	artistCount, _ := subsonic.ParseIntParam(r, "artistCount", 20)
	artistOffset, _ := subsonic.ParseIntParam(r, "artistOffset", 0)
	albumCount, _ := subsonic.ParseIntParam(r, "albumCount", 20)
	albumOffset, _ := subsonic.ParseIntParam(r, "albumOffset", 0)
	songCount, _ := subsonic.ParseIntParam(r, "songCount", 20)
	songOffset, _ := subsonic.ParseIntParam(r, "songOffset", 0)
	result, err := s.subsonicSvc.Search(r.Context(), user.ID, query, folderID,
		artistCount, artistOffset, albumCount, albumOffset, songCount, songOffset)
	if err != nil {
		return nil, err
	}
	resp := subsonic.OK(version)
	if id3 {
		resp.SearchResult3 = &result
	} else {
		resp.SearchResult2 = &subsonic.SearchResult2{Artist: result.Artist, Album: result.Album, Song: result.Song}
	}
	return resp, nil
}

func subsonicAction(raw string) string {
	raw = strings.TrimSpace(raw)
	return strings.TrimSuffix(raw, ".view")
}

func firstForm(r *http.Request, name string) string {
	_ = r.ParseForm()
	if v := r.Form.Get(name); v != "" {
		return v
	}
	return r.URL.Query().Get(name)
}
