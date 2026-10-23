package jukebox

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Sentinel errors for HTTP mapping.
var (
	ErrDisabled     = errors.New("server audio is not enabled")
	ErrForbidden    = errors.New("you do not have permission to control server audio")
	ErrConflict     = errors.New("server audio is controlled by another session")
	ErrBadSession   = errors.New("invalid server audio session")
	ErrBadPath      = errors.New("media path is outside the library")
	ErrEmptyQueue   = errors.New("server audio queue is empty")
	ErrBadIndex     = errors.New("queue index out of range")
	ErrUnavailable  = errors.New("server audio output is unavailable")
)

const (
	// MaxQueue bounds remote queue mutations (DoS / IPC pressure).
	MaxQueue = 500
	// claimIdle is how long an abandoned claim can be reclaimed.
	claimIdle = 2 * time.Minute
	// DeviceID is the stable Play-on target id for this server's speakers.
	DeviceID = "server-speakers"
)

// PathResolver maps a permitted media id to an absolute library path and
// duration. The service layer supplies permission checks before calling.
type PathResolver func(ctx context.Context, mediaID, userID int64) (path string, duration float64, err error)

// Engine owns one optional server-audio session: queue, claim, volume and
// device selection. Client streaming stays independent; this never mutates
// browser or Android local players.
type Engine struct {
	mu       sync.Mutex
	adapter  Adapter
	mediaRoot string
	resolve  PathResolver
	enabled  bool
	adminOnly bool

	queue     []int64
	index     int
	volume    int
	deviceID  string
	playing   bool
	position  float64
	duration  float64
	owner     int64
	session   string
	revision  int64
	lastTouch time.Time
}

// Options configures a new Engine.
type Options struct {
	Adapter   Adapter
	MediaRoot string
	Resolve   PathResolver
	Enabled   bool
	// AdminOnly gates mutating controls; status remains readable when false
	// only for users who may control (admin) in the service layer.
	AdminOnly bool
}

// New builds an Engine. A nil adapter with Enabled still reports the feature
// but control actions fail with ErrUnavailable until an adapter is set.
func New(opts Options) *Engine {
	root := opts.MediaRoot
	if root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
	}
	return &Engine{
		adapter:   opts.Adapter,
		mediaRoot: root,
		resolve:   opts.Resolve,
		enabled:   opts.Enabled,
		adminOnly: opts.AdminOnly,
		volume:    80,
		index:     -1,
	}
}

// Enabled reports whether the optional engine is turned on.
func (e *Engine) Enabled() bool {
	if e == nil {
		return false
	}
	return e.enabled
}

// Close releases the adapter.
func (e *Engine) Close(ctx context.Context) error {
	if e == nil || e.adapter == nil {
		return nil
	}
	return e.adapter.Close(ctx)
}

// Status is the JSON-facing snapshot.
type Status struct {
	Enabled    bool     `json:"enabled"`
	CanControl bool     `json:"can_control"`
	Name       string   `json:"name"`
	DeviceID   string   `json:"device_id"`
	State      string   `json:"state"`
	Position   float64  `json:"position"`
	Duration   float64  `json:"duration"`
	Volume     int      `json:"volume"`
	OutputID   string   `json:"output_id"`
	Queue      []int64  `json:"queue"`
	Index      int      `json:"index"`
	Devices    []Device `json:"devices"`
	Revision   int64    `json:"revision"`
}

// Snapshot returns status for a caller. canControl is decided by the caller.
func (e *Engine) Snapshot(ctx context.Context, canControl bool) (Status, error) {
	if e == nil || !e.enabled {
		return Status{}, ErrDisabled
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	st := Status{
		Enabled:    true,
		CanControl: canControl,
		Name:       "This server's speakers",
		DeviceID:   DeviceID,
		State:      "STOPPED",
		Position:   e.position,
		Duration:   e.duration,
		Volume:     e.volume,
		OutputID:   e.deviceID,
		Queue:      append([]int64(nil), e.queue...),
		Index:      e.index,
		Revision:   e.revision,
	}
	if e.playing {
		st.State = "PLAYING"
	} else if e.index >= 0 && len(e.queue) > 0 {
		st.State = "PAUSED"
	}
	if e.adapter != nil {
		if devices, err := e.adapter.Devices(ctx); err == nil {
			st.Devices = devices
		}
		if live, err := e.adapter.Status(ctx); err == nil {
			st.Position = live.Position
			if live.Duration > 0 {
				st.Duration = live.Duration
			}
			e.position = live.Position
			if live.Duration > 0 {
				e.duration = live.Duration
			}
			e.playing = live.Playing
			if live.Playing {
				st.State = "PLAYING"
			} else if e.index >= 0 && len(e.queue) > 0 {
				st.State = "PAUSED"
			} else {
				st.State = "STOPPED"
			}
		}
	}
	return st, nil
}

// Command is one remote control request.
type Command struct {
	UserID    int64
	IsAdmin   bool
	SessionID string
	Revision  int64
	Action    string
	MediaID   int64
	MediaIDs  []int64
	Index     int
	Position  float64
	Volume    int
	OutputID  string
}

// Control applies a permissioned remote command.
func (e *Engine) Control(ctx context.Context, cmd Command) (Status, error) {
	if e == nil || !e.enabled {
		return Status{}, ErrDisabled
	}
	if e.adminOnly && !cmd.IsAdmin {
		return Status{}, ErrForbidden
	}
	if raw, err := hex.DecodeString(cmd.SessionID); err != nil || len(raw) != 16 || cmd.Revision < 1 || cmd.Revision > 9007199254740991 {
		return Status{}, ErrBadSession
	}
	if cmd.Position < 0 || cmd.Position > 7*24*3600 {
		return Status{}, fmt.Errorf("%w: position", ErrBadSession)
	}
	if cmd.Volume < 0 || cmd.Volume > 100 {
		return Status{}, fmt.Errorf("%w: volume", ErrBadSession)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.claimLocked(cmd); err != nil {
		return Status{}, err
	}

	var err error
	switch cmd.Action {
	case "set_queue":
		err = e.setQueueLocked(cmd.MediaIDs)
	case "append":
		err = e.appendLocked(cmd.MediaIDs)
	case "clear":
		e.queue = nil
		e.index = -1
		e.playing = false
		e.position = 0
		e.duration = 0
		if e.adapter != nil {
			err = e.adapter.Stop(ctx)
		}
	case "load":
		err = e.loadLocked(ctx, cmd)
	case "play":
		err = e.playLocked(ctx)
	case "pause":
		if e.adapter == nil {
			err = ErrUnavailable
		} else {
			err = e.adapter.Pause(ctx)
			e.playing = false
		}
	case "seek":
		if e.adapter == nil {
			err = ErrUnavailable
		} else {
			err = e.adapter.Seek(ctx, cmd.Position)
			e.position = cmd.Position
		}
	case "stop":
		e.playing = false
		e.position = 0
		if e.adapter != nil {
			err = e.adapter.Stop(ctx)
		}
		e.releaseLocked()
	case "volume":
		if e.adapter == nil {
			err = ErrUnavailable
		} else {
			err = e.adapter.SetVolume(ctx, cmd.Volume)
			e.volume = cmd.Volume
		}
	case "set_device":
		if e.adapter == nil {
			err = ErrUnavailable
		} else {
			err = e.adapter.SetDevice(ctx, cmd.OutputID)
			e.deviceID = cmd.OutputID
		}
	case "status":
		// refresh via Snapshot path below
	default:
		return Status{}, fmt.Errorf("%w: unknown action", ErrBadSession)
	}
	if err != nil {
		return Status{}, err
	}
	e.lastTouch = time.Now()
	// Unlock briefly is avoided; Snapshot needs the lock — build inline.
	return e.snapshotLocked(ctx, true)
}

func (e *Engine) claimLocked(cmd Command) error {
	now := time.Now()
	if e.owner == 0 || e.session == "" {
		e.owner = cmd.UserID
		e.session = cmd.SessionID
		e.revision = cmd.Revision
		e.lastTouch = now
		return nil
	}
	if e.session == cmd.SessionID && e.owner == cmd.UserID {
		if cmd.Action == "load" || cmd.Action == "stop" || cmd.Action == "set_queue" || cmd.Action == "clear" {
			if cmd.Revision < e.revision {
				return ErrConflict
			}
			e.revision = cmd.Revision
		} else if cmd.Revision != e.revision {
			return ErrConflict
		}
		e.lastTouch = now
		return nil
	}
	if now.Sub(e.lastTouch) > claimIdle {
		e.owner = cmd.UserID
		e.session = cmd.SessionID
		e.revision = cmd.Revision
		e.lastTouch = now
		return nil
	}
	return ErrConflict
}

func (e *Engine) releaseLocked() {
	e.owner = 0
	e.session = ""
	e.revision = 0
}

func (e *Engine) setQueueLocked(ids []int64) error {
	if len(ids) > MaxQueue {
		return fmt.Errorf("%w: queue too long", ErrBadSession)
	}
	e.queue = append([]int64(nil), ids...)
	if len(e.queue) == 0 {
		e.index = -1
	} else if e.index < 0 || e.index >= len(e.queue) {
		e.index = 0
	}
	return nil
}

func (e *Engine) appendLocked(ids []int64) error {
	if len(e.queue)+len(ids) > MaxQueue {
		return fmt.Errorf("%w: queue too long", ErrBadSession)
	}
	e.queue = append(e.queue, ids...)
	if e.index < 0 && len(e.queue) > 0 {
		e.index = 0
	}
	return nil
}

func (e *Engine) loadLocked(ctx context.Context, cmd Command) error {
	if e.adapter == nil || e.resolve == nil {
		return ErrUnavailable
	}
	mediaID := cmd.MediaID
	if mediaID == 0 && len(cmd.MediaIDs) > 0 {
		if err := e.setQueueLocked(cmd.MediaIDs); err != nil {
			return err
		}
		if cmd.Index >= 0 && cmd.Index < len(e.queue) {
			e.index = cmd.Index
		} else {
			e.index = 0
		}
		mediaID = e.queue[e.index]
	} else if mediaID != 0 {
		// Replace queue with a single track when only media_id is given.
		if len(cmd.MediaIDs) == 0 {
			e.queue = []int64{mediaID}
			e.index = 0
		} else {
			if err := e.setQueueLocked(cmd.MediaIDs); err != nil {
				return err
			}
			e.index = indexOf(e.queue, mediaID)
			if e.index < 0 {
				e.queue = append([]int64{mediaID}, e.queue...)
				e.index = 0
			}
		}
	} else if len(e.queue) == 0 {
		return ErrEmptyQueue
	} else if cmd.Index >= 0 {
		if cmd.Index >= len(e.queue) {
			return ErrBadIndex
		}
		e.index = cmd.Index
		mediaID = e.queue[e.index]
	} else if e.index >= 0 && e.index < len(e.queue) {
		mediaID = e.queue[e.index]
	} else {
		return ErrEmptyQueue
	}

	path, duration, err := e.resolve(ctx, mediaID, cmd.UserID)
	if err != nil {
		return err
	}
	path, err = confinePath(e.mediaRoot, path)
	if err != nil {
		return err
	}
	if err := e.adapter.Load(ctx, path, cmd.Position); err != nil {
		return err
	}
	if err := e.adapter.SetVolume(ctx, e.volume); err != nil {
		return err
	}
	if e.deviceID != "" {
		_ = e.adapter.SetDevice(ctx, e.deviceID)
	}
	e.playing = true
	e.position = cmd.Position
	e.duration = duration
	return nil
}

func (e *Engine) playLocked(ctx context.Context) error {
	if e.adapter == nil {
		return ErrUnavailable
	}
	if e.index < 0 || len(e.queue) == 0 {
		return ErrEmptyQueue
	}
	if err := e.adapter.Play(ctx); err != nil {
		return err
	}
	e.playing = true
	return nil
}

func (e *Engine) snapshotLocked(ctx context.Context, canControl bool) (Status, error) {
	// Duplicate of Snapshot body while already holding e.mu.
	st := Status{
		Enabled:    true,
		CanControl: canControl,
		Name:       "This server's speakers",
		DeviceID:   DeviceID,
		State:      "STOPPED",
		Position:   e.position,
		Duration:   e.duration,
		Volume:     e.volume,
		OutputID:   e.deviceID,
		Queue:      append([]int64(nil), e.queue...),
		Index:      e.index,
		Revision:   e.revision,
	}
	if e.playing {
		st.State = "PLAYING"
	} else if e.index >= 0 && len(e.queue) > 0 {
		st.State = "PAUSED"
	}
	if e.adapter != nil {
		if devices, err := e.adapter.Devices(ctx); err == nil {
			st.Devices = devices
		}
		if live, err := e.adapter.Status(ctx); err == nil {
			st.Position = live.Position
			if live.Duration > 0 {
				st.Duration = live.Duration
			}
			if live.Playing {
				st.State = "PLAYING"
			} else if e.index >= 0 && len(e.queue) > 0 {
				st.State = "PAUSED"
			}
		}
	}
	return st, nil
}

func confinePath(root, path string) (string, error) {
	if root == "" {
		return "", ErrBadPath
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", ErrBadPath
	}
	abs = filepath.Clean(abs)
	root = filepath.Clean(root)
	sep := string(filepath.Separator)
	if abs != root && !strings.HasPrefix(abs, root+sep) {
		return "", ErrBadPath
	}
	return abs, nil
}

func indexOf(ids []int64, id int64) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}
