package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManager_EmptyDir_NoPlugins(t *testing.T) {
	m, err := NewManager(Options{Dir: "", Loader: NewWazeroLoader()})
	if err != nil {
		t.Fatal(err)
	}
	list, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %#v", list)
	}
}

func TestManager_EnableInvokeDisable(t *testing.T) {
	dir := copyFixtureTree(t, "ok-plugin")
	m, err := NewManager(Options{
		Dir:       dir,
		Loader:    NewWazeroLoader(),
		StatePath: filepath.Join(t.TempDir(), "enabled.json"),
		Limits:    Limits{CallTimeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())

	info, err := m.Enable(context.Background(), "ok-plugin")
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != StatusActive || !info.Enabled {
		t.Fatalf("enable status: %+v", info)
	}
	res, err := m.Invoke(context.Background(), "ok-plugin", CapabilityProbe)
	if err != nil {
		t.Fatal(err)
	}
	if res != 42 {
		t.Fatalf("probe result %d", res)
	}
	info, err = m.Disable(context.Background(), "ok-plugin")
	if err != nil {
		t.Fatal(err)
	}
	if info.Enabled || info.Status != StatusDisabled {
		t.Fatalf("disable status: %+v", info)
	}
	_, err = m.Invoke(context.Background(), "ok-plugin", CapabilityProbe)
	if !errors.Is(err, errDisabled) {
		t.Fatalf("want disabled, got %v", err)
	}
}

func TestManager_RejectsBadAPIVersion(t *testing.T) {
	dir := copyFixtureTree(t, "bad-version")
	m, err := NewManager(Options{
		Dir:       dir,
		Loader:    NewWazeroLoader(),
		StatePath: filepath.Join(t.TempDir(), "enabled.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Status != StatusError {
		t.Fatalf("expected error row, got %#v", list)
	}
	_, err = m.Enable(context.Background(), "bad-version")
	if err == nil || !strings.Contains(err.Error(), "api_version") {
		t.Fatalf("want api_version enable error, got %v", err)
	}
}

func TestManager_CallTimeout(t *testing.T) {
	dir := copyFixtureTree(t, "loop-plugin")
	m, err := NewManager(Options{
		Dir:       dir,
		Loader:    NewWazeroLoader(),
		StatePath: filepath.Join(t.TempDir(), "enabled.json"),
		Limits:    Limits{CallTimeout: 80 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	_, err = m.Enable(context.Background(), "loop-plugin")
	if err == nil {
		t.Fatal("expected timeout on enable of looping probe")
	}
	info, getErr := m.Get(context.Background(), "loop-plugin")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if info.Status != StatusError || info.Enabled {
		t.Fatalf("expected error/disabled after failed enable: %+v", info)
	}
}

func copyFixtureTree(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("testdata", name)
	dstRoot := t.TempDir()
	dst := filepath.Join(dstRoot, name)
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	return dstRoot
}
