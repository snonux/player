package service

import (
	"context"
	"errors"
	"strings"

	"github.com/snonux/player/internal/jukebox"
)

// JukeboxService is the optional A02a server-audio control surface.
type JukeboxService interface {
	Enabled() bool
	Status(ctx context.Context, userID int64, isAdmin bool) (jukebox.Status, error)
	Control(ctx context.Context, cmd jukebox.Command) (jukebox.Status, error)
	Close(ctx context.Context) error
}

type jukeboxService struct {
	engine *jukebox.Engine
}

// NewJukeboxService wraps an Engine. A nil engine means the feature is off.
func NewJukeboxService(engine *jukebox.Engine) JukeboxService {
	return &jukeboxService{engine: engine}
}

// JukeboxMediaAccess is the narrow browse surface the jukebox needs.
type JukeboxMediaAccess interface {
	StreamMedia(ctx context.Context, mediaID, userID int64) (*FileResult, error)
}

// NewJukeboxEngine builds the optional engine with media-root confinement and
// browse permission checks. Returns nil when enabled is false.
func NewJukeboxEngine(enabled bool, mediaRoot string, adapter jukebox.Adapter, browse JukeboxMediaAccess) *jukebox.Engine {
	if !enabled {
		return nil
	}
	return jukebox.New(jukebox.Options{
		Enabled:   true,
		AdminOnly: true,
		Adapter:   adapter,
		MediaRoot: mediaRoot,
		Resolve: func(ctx context.Context, mediaID, userID int64) (string, float64, error) {
			if browse == nil {
				return "", 0, jukebox.ErrUnavailable
			}
			res, err := browse.StreamMedia(ctx, mediaID, userID)
			if err != nil {
				return "", 0, err
			}
			if res == nil || res.Path == "" {
				return "", 0, jukebox.ErrUnavailable
			}
			if !looksLikeAudio(res.FileName) {
				return "", 0, errors.New("choose an audio track for server speakers")
			}
			return res.Path, res.Duration, nil
		},
	})
}

func looksLikeAudio(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range []string{
		".mp3", ".flac", ".ogg", ".oga", ".opus", ".m4a", ".aac", ".wav",
		".wma", ".aiff", ".aif", ".alac", ".ape", ".wv", ".mpc",
	} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func (s *jukeboxService) Enabled() bool {
	return s != nil && s.engine != nil && s.engine.Enabled()
}

func (s *jukeboxService) Status(ctx context.Context, userID int64, isAdmin bool) (jukebox.Status, error) {
	if !s.Enabled() {
		return jukebox.Status{}, jukebox.ErrDisabled
	}
	return s.engine.Snapshot(ctx, isAdmin)
}

func (s *jukeboxService) Control(ctx context.Context, cmd jukebox.Command) (jukebox.Status, error) {
	if !s.Enabled() {
		return jukebox.Status{}, jukebox.ErrDisabled
	}
	return s.engine.Control(ctx, cmd)
}

func (s *jukeboxService) Close(ctx context.Context) error {
	if s == nil || s.engine == nil {
		return nil
	}
	return s.engine.Close(ctx)
}
