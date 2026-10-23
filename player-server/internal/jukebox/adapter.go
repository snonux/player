package jukebox

import "context"

// Device is one server-local audio output the adapter can select.
type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Adapter drives the host audio backend. Implementations must refuse paths
// outside the media root the Engine already confined, keep at most one
// playback process, and honour context cancellation / Close.
type Adapter interface {
	// Devices lists selectable outputs. Empty is allowed (default device).
	Devices(ctx context.Context) ([]Device, error)
	// Load replaces the current source with path and seeks to positionSec.
	Load(ctx context.Context, path string, positionSec float64) error
	Play(ctx context.Context) error
	Pause(ctx context.Context) error
	Seek(ctx context.Context, positionSec float64) error
	// SetVolume sets linear volume in 0..100.
	SetVolume(ctx context.Context, volume int) error
	// SetDevice selects an output from Devices; empty id resets to default.
	SetDevice(ctx context.Context, deviceID string) error
	// Status reports transport state, position and duration in seconds.
	Status(ctx context.Context) (State, error)
	// Stop stops playback without releasing Engine ownership.
	Stop(ctx context.Context) error
	// Close releases the backend process and IPC resources.
	Close(ctx context.Context) error
}

// State is the adapter transport snapshot.
type State struct {
	Playing  bool
	Position float64
	Duration float64
}
