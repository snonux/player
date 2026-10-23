package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

// ErrLibraryInUse is returned when deleting a library that still owns sets.
var ErrLibraryInUse = errors.New("library still has sets; remove or rescan after emptying the root")

// ErrLastLibrary is returned when deleting the only remaining library.
var ErrLastLibrary = errors.New("cannot remove the last music library")

// ErrLibraryPathInvalid is returned for unusable library roots.
var ErrLibraryPathInvalid = errors.New("library path must be an existing directory")

// MusicLibraryStore is the persistence surface for library admin/browse.
type MusicLibraryStore interface {
	repository.MusicLibraryRepo
	repository.SetRepo
	repository.SetPermissionRepo
	repository.UserRepo
}

// MusicLibraryService manages independent roots and library-level access
// that extends existing set_permissions rows.
type MusicLibraryService struct {
	store  MusicLibraryStore
	clock  clock.Clock
	roots  *PathRoots
	browse CatalogSource
}

// NewMusicLibraryService wires library CRUD onto set ACLs and path roots.
func NewMusicLibraryService(store MusicLibraryStore, clk clock.Clock, roots *PathRoots, browse CatalogSource) *MusicLibraryService {
	return &MusicLibraryService{store: store, clock: clk, roots: roots, browse: browse}
}

// RefreshRoots reloads the streaming allowlist from the database.
func (s *MusicLibraryService) RefreshRoots(ctx context.Context) error {
	if s.roots == nil {
		return nil
	}
	libs, err := s.store.ListMusicLibraries(ctx)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(libs))
	for _, lib := range libs {
		if lib.RootPath != "" {
			paths = append(paths, lib.RootPath)
		}
	}
	s.roots.Set(paths...)
	return nil
}

// EnsureDefault attaches MEDIA_ROOT as the default library and refreshes roots.
func (s *MusicLibraryService) EnsureDefault(ctx context.Context, mediaRoot string) (*model.MusicLibrary, error) {
	lib, err := s.store.EnsureDefaultMusicLibrary(ctx, mediaRoot, s.clock.Now())
	if err != nil {
		return nil, err
	}
	if err := s.RefreshRoots(ctx); err != nil {
		return nil, err
	}
	return lib, nil
}

// ListAdmin returns all libraries with full aggregate statistics.
func (s *MusicLibraryService) ListAdmin(ctx context.Context) ([]model.MusicLibrary, error) {
	libs, err := s.store.ListMusicLibraries(ctx)
	if err != nil {
		return nil, err
	}
	for i := range libs {
		stats, err := s.store.MusicLibraryStats(ctx, libs[i].ID, nil)
		if err != nil {
			return nil, err
		}
		libs[i].Stats = stats
	}
	return libs, nil
}

// ListForUser returns libraries the caller can see (admin: all; else any
// library with at least one permitted set). Stats are ACL-filtered.
func (s *MusicLibraryService) ListForUser(ctx context.Context, userID int64) ([]model.MusicLibrary, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	libs, err := s.store.ListMusicLibraries(ctx)
	if err != nil {
		return nil, err
	}
	var allowed map[int64]struct{}
	if user == nil || !user.IsAdmin {
		sets, err := s.browse.ListSets(ctx, userID)
		if err != nil {
			return nil, err
		}
		allowed = map[int64]struct{}{}
		for _, set := range sets {
			if set.LibraryID != 0 {
				allowed[set.LibraryID] = struct{}{}
			}
		}
	}
	out := make([]model.MusicLibrary, 0, len(libs))
	for _, lib := range libs {
		if allowed != nil {
			if _, ok := allowed[lib.ID]; !ok {
				continue
			}
		}
		var setIDs []int64
		if allowed != nil {
			sets, err := s.browse.ListSets(ctx, userID)
			if err != nil {
				return nil, err
			}
			for _, set := range sets {
				if set.LibraryID == lib.ID {
					setIDs = append(setIDs, set.ID)
				}
			}
		} else {
			setIDs = nil
		}
		stats, err := s.store.MusicLibraryStats(ctx, lib.ID, setIDs)
		if err != nil {
			return nil, err
		}
		lib.Stats = stats
		out = append(out, lib)
	}
	return out, nil
}

// Create registers a new independent root and refreshes the path allowlist.
func (s *MusicLibraryService) Create(ctx context.Context, name, rootPath string) (*model.MusicLibrary, error) {
	name = strings.TrimSpace(name)
	rootPath = strings.TrimSpace(rootPath)
	if name == "" {
		return nil, &apiError{msg: "Library name is required.", status: 400}
	}
	abs, err := validateLibraryRoot(rootPath)
	if err != nil {
		return nil, err
	}
	lib := &model.MusicLibrary{Name: name, RootPath: abs, CreatedAt: s.clock.Now()}
	id, err := s.store.CreateMusicLibrary(ctx, lib)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, &apiError{msg: "A library with that path already exists.", status: 409}
		}
		return nil, err
	}
	lib.ID = id
	_ = s.RefreshRoots(ctx)
	return lib, nil
}

// Rename updates the display name.
func (s *MusicLibraryService) Rename(ctx context.Context, id int64, name string) (*model.MusicLibrary, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, &apiError{msg: "Library name is required.", status: 400}
	}
	lib, err := s.store.GetMusicLibrary(ctx, id)
	if err != nil || lib == nil {
		return nil, &apiError{msg: "Library not found.", status: 404}
	}
	lib.Name = name
	if err := s.store.UpdateMusicLibrary(ctx, lib); err != nil {
		return nil, err
	}
	return lib, nil
}

// Delete removes an empty non-last library.
func (s *MusicLibraryService) Delete(ctx context.Context, id int64) error {
	libs, err := s.store.ListMusicLibraries(ctx)
	if err != nil {
		return err
	}
	if len(libs) <= 1 {
		return ErrLastLibrary
	}
	lib, err := s.store.GetMusicLibrary(ctx, id)
	if err != nil || lib == nil {
		return &apiError{msg: "Library not found.", status: 404}
	}
	ids, err := s.store.ListSetIDsByLibrary(ctx, id)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		return ErrLibraryInUse
	}
	if err := s.store.DeleteMusicLibrary(ctx, id); err != nil {
		return err
	}
	return s.RefreshRoots(ctx)
}

// GrantAccess applies role to every set in the library (extends set ACL).
func (s *MusicLibraryService) GrantAccess(ctx context.Context, libraryID, userID int64, role model.Role) error {
	if role != model.RoleOwner && role != model.RoleViewer {
		return &apiError{msg: "Role must be owner or viewer.", status: 400}
	}
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		return &apiError{msg: "User not found.", status: 404}
	}
	lib, err := s.store.GetMusicLibrary(ctx, libraryID)
	if err != nil || lib == nil {
		return &apiError{msg: "Library not found.", status: 404}
	}
	ids, err := s.store.ListSetIDsByLibrary(ctx, libraryID)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	for _, setID := range ids {
		if err := s.store.GrantPermission(ctx, &model.SetPermission{
			SetID: setID, UserID: userID, Role: role, CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("grant set %d: %w", setID, err)
		}
	}
	return nil
}

// RevokeAccess removes the user from every set in the library.
func (s *MusicLibraryService) RevokeAccess(ctx context.Context, libraryID, userID int64) error {
	ids, err := s.store.ListSetIDsByLibrary(ctx, libraryID)
	if err != nil {
		return err
	}
	for _, setID := range ids {
		if err := s.store.RevokePermission(ctx, setID, userID); err != nil {
			return fmt.Errorf("revoke set %d: %w", setID, err)
		}
	}
	return nil
}

func validateLibraryRoot(rootPath string) (string, error) {
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return "", &apiError{msg: ErrLibraryPathInvalid.Error(), status: 400}
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", &apiError{msg: ErrLibraryPathInvalid.Error(), status: 400}
	}
	return abs, nil
}

// FilterSetsByLibraries keeps sets whose library_id is in the selection.
// Empty libraryIDs means no filter (all sets).
func FilterSetsByLibraries(sets []model.Set, libraryIDs []int64) []model.Set {
	if len(libraryIDs) == 0 {
		return sets
	}
	want := map[int64]struct{}{}
	for _, id := range libraryIDs {
		want[id] = struct{}{}
	}
	out := make([]model.Set, 0, len(sets))
	for _, set := range sets {
		if _, ok := want[set.LibraryID]; ok {
			out = append(out, set)
		}
	}
	return out
}

// ParseLibraryIDs reads comma-separated library_ids from a query value.
func ParseLibraryIDs(raw string) ([]int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var id int64
		if _, err := fmt.Sscan(part, &id); err != nil || id <= 0 {
			return nil, fmt.Errorf("Invalid library_ids.")
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// MusicAlbumsInLibraries builds albums after ACL and optional library filter.
func MusicAlbumsInLibraries(ctx context.Context, source CatalogSource, userID int64, libraryIDs []int64) ([]Album, error) {
	albums, err := MusicAlbums(ctx, source, userID)
	if err != nil {
		return nil, err
	}
	if len(libraryIDs) == 0 {
		return albums, nil
	}
	sets, err := source.ListSets(ctx, userID)
	if err != nil {
		return nil, err
	}
	allowed := map[int64]struct{}{}
	for _, set := range FilterSetsByLibraries(sets, libraryIDs) {
		allowed[set.ID] = struct{}{}
	}
	out := make([]Album, 0, len(albums))
	for _, album := range albums {
		songs := make([]model.Media, 0, len(album.Songs))
		for _, song := range album.Songs {
			if _, ok := allowed[song.SetID]; ok {
				songs = append(songs, song)
			}
		}
		if len(songs) == 0 {
			continue
		}
		album.Songs = songs
		album.SongCount = len(songs)
		album.Duration = 0
		album.ListeningStats = model.ListeningStats{}
		for _, song := range songs {
			album.Duration += song.Duration
			album.ListeningStats.Add(song.ListeningStats)
		}
		out = append(out, album)
	}
	return out, nil
}
