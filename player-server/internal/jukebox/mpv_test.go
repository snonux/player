package jukebox

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestFakeMPVHelper is not a test: run as a child process it plays mpv's
// IPC side, answering every request with success.
func TestFakeMPVHelper(t *testing.T) {
	socket := os.Getenv("PLAYER_FAKE_MPV_SOCKET")
	if socket == "" {
		t.Skip("helper process only")
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		os.Exit(2)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			os.Exit(0)
		}
		go func(c net.Conn) {
			r := bufio.NewReader(c)
			for {
				line, err := r.ReadBytes('\n')
				if err != nil {
					return
				}
				var req struct {
					RequestID int `json:"request_id"`
				}
				_ = json.Unmarshal(line, &req)
				// An unsolicited event in the same write as the reply must
				// not be lost for the next request.
				resp, _ := json.Marshal(map[string]any{"request_id": req.RequestID, "error": "success", "data": false})
				_, _ = c.Write(append(append([]byte(`{"event":"idle"}`+"\n"), resp...), '\n'))
			}
		}(conn)
	}
}

func fakeMPV(t *testing.T) (*MPV, *[]int) {
	t.Helper()
	dir, err := os.MkdirTemp("", "mpv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	var pids []int
	m := &MPV{bin: os.Args[0], sock: filepath.Join(dir, "s")}
	m.launch = func(socket string) (*exec.Cmd, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestFakeMPVHelper$")
		cmd.Env = append(os.Environ(), "PLAYER_FAKE_MPV_SOCKET="+socket)
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		pids = append(pids, cmd.Process.Pid)
		return cmd, nil
	}
	t.Cleanup(func() { _ = m.Close(context.Background()) })
	return m, &pids
}

// zombie reports whether pid is an unreaped child.
func zombie(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	fields := strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+1:]))
	return len(fields) > 0 && fields[0] == "Z"
}

// B3: after mpv dies the adapter starts a new one instead of failing
// forever, and the dead process is reaped.
func TestMPV_RestartsAfterProcessDies(t *testing.T) {
	m, pids := fakeMPV(t)
	ctx := context.Background()
	if err := m.Play(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Pause(ctx); err != nil {
		t.Fatalf("second request on the same connection: %v", err)
	}
	first := (*pids)[0]
	if err := syscall.Kill(first, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	var err error
	for i := 0; i < 3; i++ {
		if err = m.Play(ctx); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("play after mpv died: %v", err)
	}
	if len(*pids) != 2 {
		t.Fatalf("mpv started %d times, want 2", len(*pids))
	}
	if zombie(first) {
		t.Fatalf("dead mpv %d left as zombie", first)
	}
}
