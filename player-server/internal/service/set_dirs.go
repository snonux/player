package service

import (
	"context"
	"path/filepath"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/thumb"
)

// musicLibraryGetter is the store method needed to find a set's library.
type musicLibraryGetter interface {
	GetMusicLibrary(ctx context.Context, id int64) (*model.MusicLibrary, error)
}

// setLibraryRoot returns the root directory of the library set belongs to.
//
// Since multi-library support two libraries may hold sets with the same
// RootPath, so a set directory must never be derived from the single
// default media root. Sets without a library (podcast sets, rows from before
// libraries existed) and stores that know no libraries fall back to
// defaultRoot.
func setLibraryRoot(ctx context.Context, store any, defaultRoot string, set *model.Set) (string, error) {
	if set == nil || set.LibraryID == 0 {
		return defaultRoot, nil
	}
	libs, ok := store.(musicLibraryGetter)
	if !ok {
		return defaultRoot, nil
	}
	lib, err := libs.GetMusicLibrary(ctx, set.LibraryID)
	if err != nil {
		return "", err
	}
	if lib == nil || lib.RootPath == "" {
		return defaultRoot, nil
	}
	return lib.RootPath, nil
}

// setDirectoryIn returns the set's library root and its directory below it.
func setDirectoryIn(ctx context.Context, store any, defaultRoot string, set *model.Set) (root, dir string, err error) {
	root, err = setLibraryRoot(ctx, store, defaultRoot, set)
	if err != nil {
		return "", "", err
	}
	return root, filepath.Clean(filepath.Join(root, filepath.FromSlash(set.RootPath))), nil
}

// coverLayoutFor returns the thumbnail layout for covers of a set whose
// library lives at root. The configured layout (and its cache, which
// mirrors only the default media root) applies to the default library.
// For any other library a mirrored cache path would collide with the
// same-named set of the default library, so its covers live beside the
// set's own files instead.
func coverLayoutFor(layout thumb.Layout, defaultRoot, root string) thumb.Layout {
	if root == "" || sameDir(root, defaultRoot) {
		return layout
	}
	return thumb.NewLayout(root, "")
}

func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return absA == absB
}

type setGetter interface {
	GetSetByID(ctx context.Context, id int64) (*model.Set, error)
}

// mediaLibraryRoot returns the root of the library media belongs to, the
// directory its local files (sidecar lyrics, format probes) are confined
// to. Whenever the set or library cannot be resolved it answers
// defaultRoot, which keeps media of the default library working and simply
// finds nothing for media elsewhere.
func mediaLibraryRoot(ctx context.Context, store any, defaultRoot string, media *model.Media) string {
	sets, ok := store.(setGetter)
	if !ok || media == nil {
		return defaultRoot
	}
	set, err := sets.GetSetByID(ctx, media.SetID)
	if err != nil || set == nil {
		return defaultRoot
	}
	root, err := setLibraryRoot(ctx, store, defaultRoot, set)
	if err != nil {
		return defaultRoot
	}
	return root
}
