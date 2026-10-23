package jukebox

import (
	"context"
	"sync"
)

// FakeAdapter records commands for tests and simulates simple playback.
type FakeAdapter struct {
	mu       sync.Mutex
	devices  []Device
	path     string
	volume   int
	deviceID string
	playing  bool
	position float64
	duration float64
	closed   bool
	Loads    []string
	Commands []string
}

// NewFake returns a FakeAdapter with a default device and 100 volume.
func NewFake() *FakeAdapter {
	return &FakeAdapter{
		devices: []Device{{ID: "default", Name: "Default"}, {ID: "hdmi", Name: "HDMI"}},
		volume:  100,
	}
}

// Devices implements Adapter.
func (f *FakeAdapter) Devices(context.Context) ([]Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Device, len(f.devices))
	copy(out, f.devices)
	return out, nil
}

// Load implements Adapter.
func (f *FakeAdapter) Load(_ context.Context, path string, positionSec float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Loads = append(f.Loads, path)
	f.Commands = append(f.Commands, "load")
	f.path = path
	f.position = positionSec
	if f.duration <= 0 {
		f.duration = 180
	}
	f.playing = true
	return nil
}

// Play implements Adapter.
func (f *FakeAdapter) Play(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Commands = append(f.Commands, "play")
	f.playing = true
	return nil
}

// Pause implements Adapter.
func (f *FakeAdapter) Pause(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Commands = append(f.Commands, "pause")
	f.playing = false
	return nil
}

// Seek implements Adapter.
func (f *FakeAdapter) Seek(_ context.Context, positionSec float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Commands = append(f.Commands, "seek")
	f.position = positionSec
	return nil
}

// SetVolume implements Adapter.
func (f *FakeAdapter) SetVolume(_ context.Context, volume int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Commands = append(f.Commands, "volume")
	f.volume = volume
	return nil
}

// SetDevice implements Adapter.
func (f *FakeAdapter) SetDevice(_ context.Context, deviceID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Commands = append(f.Commands, "device")
	f.deviceID = deviceID
	return nil
}

// Status implements Adapter.
func (f *FakeAdapter) Status(context.Context) (State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return State{Playing: f.playing, Position: f.position, Duration: f.duration}, nil
}

// Stop implements Adapter.
func (f *FakeAdapter) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Commands = append(f.Commands, "stop")
	f.playing = false
	f.path = ""
	return nil
}

// Close implements Adapter.
func (f *FakeAdapter) Close(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// SetDuration sets the reported duration for subsequent Status calls.
func (f *FakeAdapter) SetDuration(seconds float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.duration = seconds
}

// Volume returns the last volume set on the fake.
func (f *FakeAdapter) Volume() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.volume
}

// DeviceID returns the last selected device id.
func (f *FakeAdapter) DeviceID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deviceID
}
