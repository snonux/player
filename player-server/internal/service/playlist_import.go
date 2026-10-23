package service

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/snonux/player/internal/model"
)

// PlaylistImportRequest identifies an explicit server-local source, never a URL.
type PlaylistImportRequest struct {
	Path        string `json:"path"`
	Base        string `json:"base"`
	Follow      bool   `json:"follow"`
	Preview     bool   `json:"preview"`
	Fingerprint string `json:"fingerprint"`
}

// PlaylistImportPreview discloses counts, never unavailable or inaccessible names.
type PlaylistImportPreview struct {
	Name             string        `json:"name"`
	Available        int           `json:"available"`
	Unavailable      int           `json:"unavailable"`
	Rejected         int           `json:"rejected"`
	UnavailableLines []int         `json:"unavailable_lines"`
	Fingerprint      string        `json:"fingerprint"`
	Playlist         *PlaylistView `json:"playlist,omitempty"`
}

// WithImportRoot configures filesystem import before serving requests.
func (s *PlaylistService) WithImportRoot(root string) *PlaylistService { s.mediaRoot = root; return s }

// ImportM3U previews or creates a private playlist from the reviewed source.
func (s *PlaylistService) ImportM3U(ctx context.Context, user int64, req PlaylistImportRequest) (*PlaylistImportPreview, error) {
	s.importMu.Lock()
	result, entries, err := s.readM3U(ctx, user, req.Path, req.Base)
	if err != nil {
		s.importMu.Unlock()
		return nil, err
	}
	if req.Preview {
		s.importMu.Unlock()
		return result, nil
	}
	if req.Fingerprint == "" || req.Fingerprint != result.Fingerprint {
		s.importMu.Unlock()
		return nil, &apiError{msg: "Source or available songs changed. Preview again before importing.", status: 409}
	}
	id, err := playlistID("pl")
	if err != nil {
		s.importMu.Unlock()
		return nil, err
	}
	p := model.Playlist{ID: id, Name: result.Name, Entries: entries}
	if req.Follow {
		p.Source = &model.PlaylistSource{Path: req.Path, Base: req.Base, Status: "ready", Unavailable: result.Unavailable, Rejected: result.Rejected}
	}
	_, err = s.repo.ImportPlaylist(ctx, user, p, true)
	s.importMu.Unlock()
	if err != nil {
		return nil, err
	}
	result.Playlist, err = s.Get(ctx, user, id)
	return result, err
}

// SyncSources refreshes all explicit bindings after a completed library rescan.
func (s *PlaylistService) SyncSources(ctx context.Context) error {
	bindings, err := s.repo.PlaylistSources(ctx)
	if err != nil {
		return err
	}
	for _, p := range bindings {
		if err := s.refreshSource(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// SyncPlaylist refreshes as the current owner, or detaches as an editor/admin.
func (s *PlaylistService) SyncPlaylist(ctx context.Context, user int64, id string, detach bool) (*PlaylistView, error) {
	p, err := s.repo.GetPlaylist(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrNotFound
	}
	if !p.CanEdit {
		return nil, ErrForbidden
	}
	if detach {
		if _, err := s.repo.DetachPlaylistSource(ctx, user, id); err != nil {
			return nil, err
		}
	} else {
		if p.OwnerID != user {
			return nil, &apiError{msg: "Only the playlist owner can refresh this source.", status: 403}
		}
		if err := s.refreshSource(ctx, *p); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, user, id)
}

func (s *PlaylistService) refreshSource(ctx context.Context, p model.Playlist) error {
	if p.Source == nil {
		return nil
	}
	s.importMu.Lock()
	defer s.importMu.Unlock()
	result, entries, err := s.readM3U(ctx, p.OwnerID, p.Source.Path, p.Source.Base)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		if _, expected := err.(*apiError); !expected {
			return err
		}
		p.Source.Status = "unavailable"
	} else {
		p.Source.Status = "ready"
		p.Source.Unavailable = result.Unavailable
		p.Source.Rejected = result.Rejected
		p.Entries = entries
	}
	_, err = s.repo.ImportPlaylist(ctx, p.OwnerID, p, false)
	return err
}

func (s *PlaylistService) readM3U(ctx context.Context, user int64, source, base string) (*PlaylistImportPreview, []model.PlaylistEntry, error) {
	invalid := func() (*PlaylistImportPreview, []model.PlaylistEntry, error) {
		return nil, nil, &apiError{msg: "Source unavailable. Choose a readable UTF-8 M3U or M3U8 file inside a set you can access.", status: 400}
	}
	if s.mediaRoot == "" || !m3uLocalPath(source) || len(source) > 4096 || (base != "folder" && base != "library") {
		return invalid()
	}
	ext := strings.ToLower(path.Ext(source))
	if ext != ".m3u" && ext != ".m3u8" {
		return invalid()
	}
	sets, err := s.source.ListSets(ctx, user)
	if err != nil {
		return nil, nil, err
	}
	setRoot := ""
	for _, set := range sets {
		if m3uLocalPath(set.RootPath) && !strings.Contains(set.RootPath, "/") && strings.HasPrefix(source, set.RootPath+"/") {
			setRoot = set.RootPath
			break
		}
	}
	if setRoot == "" {
		return invalid()
	}
	// Root limits symlink resolution to the authorized set. Reject lexical
	// symlinks as well, including the set itself; never read another set via links.
	if !plainLibraryPath(s.mediaRoot, source) {
		return invalid()
	}
	setPath := filepath.Join(s.mediaRoot, filepath.FromSlash(setRoot))
	before, err := os.Lstat(setPath)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return invalid()
	}
	root, err := os.OpenRoot(setPath)
	if err != nil {
		return invalid()
	}
	defer func() { _ = root.Close() }()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		return invalid()
	}
	f, err := root.OpenFile(strings.TrimPrefix(source, setRoot+"/"), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return invalid()
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return invalid()
	}
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 || !utf8.Valid(data) {
		return invalid()
	}
	songs, err := s.visibleSongs(ctx, user)
	if err != nil {
		return nil, nil, err
	}
	paths := map[string]int64{}
	roots := map[int64]string{}
	for _, set := range sets {
		roots[set.ID] = set.RootPath
	}
	for _, song := range songs {
		r := roots[song.SetID]
		if m3uLocalPath(r) && m3uLocalPath(song.RelPath) {
			paths[path.Join(r, song.RelPath)] = song.ID
		}
	}
	result := &PlaylistImportPreview{Name: strings.TrimSuffix(path.Base(source), path.Ext(source)), UnavailableLines: []int{}}
	entries := []model.PlaylistEntry{}
	scanner := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(string(data), "\uFEFF")))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	line, count := 0, 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, "#") {
			if strings.HasPrefix(text, "#PLAYLIST:") {
				name := strings.TrimSpace(strings.TrimPrefix(text, "#PLAYLIST:"))
				if _, err := validatePlaylist(name, ""); err == nil {
					result.Name = name
				}
			}
			continue
		}
		count++
		if count > 10000 {
			return invalid()
		}
		rel, ok := resolveM3UPath(source, base, text)
		if !ok {
			result.Rejected++
			continue
		}
		mediaID, ok := paths[rel]
		if !ok || !plainLibraryPath(s.mediaRoot, rel) {
			result.Unavailable++
			if len(result.UnavailableLines) < 30 {
				result.UnavailableLines = append(result.UnavailableLines, line)
			}
			continue
		}
		id, err := playlistID("pe")
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, model.PlaylistEntry{ID: id, MediaID: mediaID})
		result.Available++
	}
	if scanner.Err() != nil {
		return invalid()
	}
	if _, err := validatePlaylist(result.Name, ""); err != nil {
		result.Name = "Imported playlist"
	}
	// Include the resolved accessible membership in the review token, so ACL or
	// index changes between preview and confirmation require another preview.
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%d\x00%d", source, base, data, result.Unavailable, result.Rejected)
	for _, e := range entries {
		_, _ = fmt.Fprintf(hash, "/%d", e.MediaID)
	}
	result.Fingerprint = fmt.Sprintf("%x", hash.Sum(nil))
	return result, entries, nil
}

func resolveM3UPath(source, base, text string) (string, bool) {
	if text == "" || strings.HasPrefix(text, "/") || strings.Contains(text, "\\") || strings.IndexFunc(text, unicode.IsControl) >= 0 {
		return "", false
	}
	// A leading ./ makes URI-like *local* names unambiguous, matching M32.
	if !strings.HasPrefix(text, "./") && strings.Contains(strings.Split(text, "/")[0], ":") {
		return "", false
	}
	rel := text
	if base == "folder" {
		rel = path.Join(path.Dir(source), text)
	} else {
		rel = path.Clean(text)
	}
	if !m3uLocalPath(rel) {
		return "", false
	}
	return rel, true
}

func plainLibraryPath(root, rel string) bool {
	current := root
	for _, part := range strings.Split(rel, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	info, err := os.Stat(current)
	return err == nil && info.Mode().IsRegular()
}
