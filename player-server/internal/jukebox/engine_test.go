package jukebox_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/snonux/player/internal/jukebox"
)

func TestEngine_Disabled(t *testing.T) {
	e := jukebox.New(jukebox.Options{Enabled: false, Adapter: jukebox.NewFake()})
	if _, err := e.Snapshot(context.Background(), true); err != jukebox.ErrDisabled {
		t.Fatalf("Snapshot: got %v", err)
	}
}

func TestEngine_PermissionAndIndependence(t *testing.T) {
	root := t.TempDir()
	track := filepath.Join(root, "a.mp3")
	fake := jukebox.NewFake()
	fake.SetDuration(30)
	e := jukebox.New(jukebox.Options{
		Enabled:   true,
		AdminOnly: true,
		Adapter:   fake,
		MediaRoot: root,
		Resolve: func(ctx context.Context, mediaID, userID int64) (string, float64, error) {
			return track, 30, nil
		},
	})
	session := "0123456789abcdef0123456789abcdef"
	_, err := e.Control(context.Background(), jukebox.Command{
		UserID: 2, IsAdmin: false, SessionID: session, Revision: 1, Action: "load", MediaID: 7,
	})
	if err != jukebox.ErrForbidden {
		t.Fatalf("non-admin: got %v", err)
	}
	st, err := e.Control(context.Background(), jukebox.Command{
		UserID: 1, IsAdmin: true, SessionID: session, Revision: 1, Action: "load", MediaID: 7, Position: 2,
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if st.State != "PLAYING" || st.Index != 0 || len(st.Queue) != 1 || st.Queue[0] != 7 {
		t.Fatalf("status after load: %+v", st)
	}
	if len(fake.Loads) != 1 || fake.Loads[0] != track {
		t.Fatalf("adapter loads: %v", fake.Loads)
	}
	if _, err := e.Control(context.Background(), jukebox.Command{
		UserID: 1, IsAdmin: true, SessionID: session, Revision: 1, Action: "volume", Volume: 40,
	}); err != nil {
		t.Fatalf("volume: %v", err)
	}
	if fake.Volume() != 40 {
		t.Fatalf("volume=%d", fake.Volume())
	}
	if _, err := e.Control(context.Background(), jukebox.Command{
		UserID: 1, IsAdmin: true, SessionID: session, Revision: 1, Action: "set_device", OutputID: "hdmi",
	}); err != nil {
		t.Fatalf("device: %v", err)
	}
	if fake.DeviceID() != "hdmi" {
		t.Fatalf("device=%s", fake.DeviceID())
	}
	if _, err := e.Control(context.Background(), jukebox.Command{
		UserID: 1, IsAdmin: true, SessionID: session, Revision: 1, Action: "seek", Position: 12,
	}); err != nil {
		t.Fatalf("seek: %v", err)
	}
	other := "fedcba9876543210fedcba9876543210"
	if _, err := e.Control(context.Background(), jukebox.Command{
		UserID: 3, IsAdmin: true, SessionID: other, Revision: 1, Action: "pause",
	}); err != jukebox.ErrConflict {
		t.Fatalf("foreign session: got %v", err)
	}
	if _, err := e.Control(context.Background(), jukebox.Command{
		UserID: 1, IsAdmin: true, SessionID: session, Revision: 2, Action: "stop",
	}); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestEngine_QueueAppendAndPathConfine(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "ok.flac")
	fake := jukebox.NewFake()
	e := jukebox.New(jukebox.Options{
		Enabled: true, AdminOnly: true, Adapter: fake, MediaRoot: root,
		Resolve: func(ctx context.Context, mediaID, userID int64) (string, float64, error) {
			if mediaID == 99 {
				return "/etc/passwd", 1, nil
			}
			return inside, 10, nil
		},
	})
	session := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := e.Control(context.Background(), jukebox.Command{
		UserID: 1, IsAdmin: true, SessionID: session, Revision: 1,
		Action: "set_queue", MediaIDs: []int64{1, 2, 3},
	}); err != nil {
		t.Fatalf("set_queue: %v", err)
	}
	if _, err := e.Control(context.Background(), jukebox.Command{
		UserID: 1, IsAdmin: true, SessionID: session, Revision: 1,
		Action: "append", MediaIDs: []int64{4},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	st, err := e.Snapshot(context.Background(), true)
	if err != nil || len(st.Queue) != 4 {
		t.Fatalf("queue: %+v %v", st, err)
	}
	if _, err := e.Control(context.Background(), jukebox.Command{
		UserID: 1, IsAdmin: true, SessionID: session, Revision: 2,
		Action: "load", MediaID: 99,
	}); err != jukebox.ErrBadPath {
		t.Fatalf("confine: got %v", err)
	}
	if len(fake.Loads) != 0 {
		t.Fatalf("escaped load: %v", fake.Loads)
	}
}

func TestEngine_NilEngine(t *testing.T) {
	var e *jukebox.Engine
	if e.Enabled() {
		t.Fatal("nil enabled")
	}
	if err := e.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
