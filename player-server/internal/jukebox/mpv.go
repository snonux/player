package jukebox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	mpvIPCTimeout = 3 * time.Second
	mpvStartWait  = 5 * time.Second
)

// MPV is a bounded Adapter that talks to one idle mpv process over a UNIX
// IPC socket. Media paths must already be confined by Engine.
type MPV struct {
	mu     sync.Mutex
	bin    string
	sock   string
	cmd    *exec.Cmd
	conn   net.Conn
	reqID  int
	closed bool
}

// NewMPV prepares an MPV adapter. The process starts lazily on first use.
// bin defaults to "mpv" when empty.
func NewMPV(bin string) (*MPV, error) {
	if bin == "" {
		bin = "mpv"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("jukebox: mpv not found: %w", err)
	}
	dir, err := os.MkdirTemp("", "player-jukebox-*")
	if err != nil {
		return nil, err
	}
	return &MPV{bin: path, sock: filepath.Join(dir, "mpv.sock")}, nil
}

// Devices implements Adapter via audio-device-list.
func (m *MPV) Devices(ctx context.Context) ([]Device, error) {
	raw, err := m.getProperty(ctx, "audio-device-list")
	if err != nil {
		return []Device{{ID: "auto", Name: "Default"}}, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return []Device{{ID: "auto", Name: "Default"}}, nil
	}
	out := make([]Device, 0, len(list))
	for _, item := range list {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := row["name"].(string)
		desc, _ := row["description"].(string)
		if name == "" {
			continue
		}
		if desc == "" {
			desc = name
		}
		out = append(out, Device{ID: name, Name: desc})
	}
	if len(out) == 0 {
		out = []Device{{ID: "auto", Name: "Default"}}
	}
	return out, nil
}

// Load implements Adapter.
func (m *MPV) Load(ctx context.Context, path string, positionSec float64) error {
	if err := m.command(ctx, "loadfile", path, "replace"); err != nil {
		return err
	}
	if positionSec > 0 {
		if err := m.command(ctx, "seek", positionSec, "absolute"); err != nil {
			return err
		}
	}
	return m.command(ctx, "set_property", "pause", false)
}

// Play implements Adapter.
func (m *MPV) Play(ctx context.Context) error {
	return m.command(ctx, "set_property", "pause", false)
}

// Pause implements Adapter.
func (m *MPV) Pause(ctx context.Context) error {
	return m.command(ctx, "set_property", "pause", true)
}

// Seek implements Adapter.
func (m *MPV) Seek(ctx context.Context, positionSec float64) error {
	return m.command(ctx, "seek", positionSec, "absolute")
}

// SetVolume implements Adapter.
func (m *MPV) SetVolume(ctx context.Context, volume int) error {
	return m.command(ctx, "set_property", "volume", volume)
}

// SetDevice implements Adapter.
func (m *MPV) SetDevice(ctx context.Context, deviceID string) error {
	if deviceID == "" {
		deviceID = "auto"
	}
	return m.command(ctx, "set_property", "audio-device", deviceID)
}

// Status implements Adapter.
func (m *MPV) Status(ctx context.Context) (State, error) {
	paused, _ := m.getProperty(ctx, "pause")
	pos, _ := m.getProperty(ctx, "time-pos")
	dur, _ := m.getProperty(ctx, "duration")
	st := State{}
	if b, ok := paused.(bool); ok {
		st.Playing = !b
	}
	st.Position = asFloat(pos)
	st.Duration = asFloat(dur)
	return st, nil
}

// Stop implements Adapter.
func (m *MPV) Stop(ctx context.Context) error {
	return m.command(ctx, "stop")
}

// Close implements Adapter.
func (m *MPV) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	if m.conn != nil {
		_ = m.conn.Close()
		m.conn = nil
	}
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		_, _ = m.cmd.Process.Wait()
		m.cmd = nil
	}
	if m.sock != "" {
		dir := filepath.Dir(m.sock)
		_ = os.Remove(m.sock)
		_ = os.Remove(dir)
	}
	return nil
}

func (m *MPV) command(ctx context.Context, name string, args ...any) error {
	if err := m.ensure(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.conn == nil {
		return ErrUnavailable
	}
	m.reqID++
	id := m.reqID
	payload := map[string]any{"command": append([]any{name}, args...), "request_id": id}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	_ = m.conn.SetDeadline(time.Now().Add(mpvIPCTimeout))
	if _, err := m.conn.Write(body); err != nil {
		return err
	}
	reader := bufio.NewReader(m.conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return err
		}
		var resp struct {
			RequestID int    `json:"request_id"`
			Error     string `json:"error"`
		}
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if resp.RequestID != id {
			continue
		}
		if resp.Error != "" && resp.Error != "success" {
			return fmt.Errorf("mpv: %s", resp.Error)
		}
		return nil
	}
}

func (m *MPV) getProperty(ctx context.Context, name string) (any, error) {
	if err := m.ensure(ctx); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.conn == nil {
		return nil, ErrUnavailable
	}
	m.reqID++
	id := m.reqID
	payload := map[string]any{"command": []any{"get_property", name}, "request_id": id}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	body = append(body, '\n')
	_ = m.conn.SetDeadline(time.Now().Add(mpvIPCTimeout))
	if _, err := m.conn.Write(body); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(m.conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var resp struct {
			RequestID int    `json:"request_id"`
			Error     string `json:"error"`
			Data      any    `json:"data"`
		}
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if resp.RequestID != id {
			continue
		}
		if resp.Error != "" && resp.Error != "success" {
			return nil, fmt.Errorf("mpv: %s", resp.Error)
		}
		return resp.Data, nil
	}
}

func (m *MPV) ensure(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrUnavailable
	}
	if m.conn != nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	return m.start(ctx)
}

// startProcess launches idle mpv. Named for the ffsafe command allowlist.
func (m *MPV) startProcess(socket string) (*exec.Cmd, error) {
	cmd := exec.Command(m.bin,
		"--idle=yes",
		"--no-video",
		"--no-terminal",
		"--really-quiet",
		"--audio-display=no",
		"--input-ipc-server="+socket,
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func (m *MPV) start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrUnavailable
	}
	if m.conn != nil {
		return nil
	}
	cmd, err := m.startProcess(m.sock)
	if err != nil {
		return err
	}
	m.cmd = cmd
	deadline := time.Now().Add(mpvStartWait)
	var conn net.Conn
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			_ = cmd.Process.Kill()
			return ctx.Err()
		}
		conn, err = net.DialTimeout("unix", m.sock, 200*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if conn == nil {
		_ = cmd.Process.Kill()
		return errors.New("jukebox: mpv ipc socket did not become ready")
	}
	m.conn = conn
	return nil
}

func asFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	default:
		return 0
	}
}
