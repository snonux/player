package plugin

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeModule counts calls in flight and can be told to fail.
type fakeModule struct {
	inFlight, peak *atomic.Int32
	fail           *atomic.Bool
	closed         atomic.Bool
}

func (f *fakeModule) Call(ctx context.Context, export string) (uint64, error) {
	if f.closed.Load() {
		return 0, errors.New("module closed with context deadline exceeded")
	}
	n := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	time.Sleep(5 * time.Millisecond)
	if f.fail.Load() {
		f.fail.Store(false)
		f.closed.Store(true) // like wazero's CloseOnContextDone
		return 0, context.DeadlineExceeded
	}
	return 42, nil
}

func (f *fakeModule) Close(context.Context) error { f.closed.Store(true); return nil }

type fakeLoader struct {
	loads          atomic.Int32
	inFlight, peak atomic.Int32
	fail           atomic.Bool
}

func (l *fakeLoader) Instantiate(context.Context, []byte, Limits) (Module, error) {
	l.loads.Add(1)
	return &fakeModule{inFlight: &l.inFlight, peak: &l.peak, fail: &l.fail}, nil
}

func fakeManager(t *testing.T) (*Manager, *fakeLoader) {
	t.Helper()
	loader := &fakeLoader{}
	m, err := NewManager(Options{Dir: copyFixtureTree(t, "ok-plugin"), Loader: loader,
		StatePath: filepath.Join(t.TempDir(), "enabled.json"), Limits: Limits{CallTimeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close(context.Background()) })
	if _, err := m.Enable(context.Background(), "ok-plugin"); err != nil {
		t.Fatal(err)
	}
	return m, loader
}

// B13: parallel invokes of one plugin never run concurrently on its
// instance, and the id is matched case-insensitively.
func TestManager_InvokeSerialisesPerPlugin(t *testing.T) {
	m, loader := fakeManager(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.Invoke(context.Background(), "ok-plugin", CapabilityProbe); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if p := loader.peak.Load(); p != 1 {
		t.Fatalf("peak calls in flight = %d, want 1", p)
	}
	if res, err := m.Invoke(context.Background(), " OK-Plugin ", CapabilityProbe); err != nil || res != 42 {
		t.Fatalf("mixed-case id: %d, %v", res, err)
	}
}

// B14: a call that fails (and closes the instance) does not leave the
// closed instance cached; the next call reloads the plugin.
func TestManager_FailedCallReloads(t *testing.T) {
	m, loader := fakeManager(t)
	loader.fail.Store(true)
	if _, err := m.Invoke(context.Background(), "ok-plugin", CapabilityProbe); err == nil {
		t.Fatal("first call should fail")
	}
	before := loader.loads.Load()
	for i := 0; i < 2; i++ {
		if res, err := m.Invoke(context.Background(), "ok-plugin", CapabilityProbe); err != nil || res != 42 {
			t.Fatalf("call %d after failure: %d, %v", i, res, err)
		}
	}
	if loader.loads.Load() != before+1 {
		t.Fatalf("loads %d → %d, want one reload", before, loader.loads.Load())
	}
	if info, _ := m.Get(context.Background(), "ok-plugin"); !info.Enabled || info.Status != StatusActive {
		t.Fatalf("status after recovery: %+v", info)
	}
}
