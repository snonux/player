package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Status values reported to the admin API/UI.
const (
	StatusDisabled = "disabled"
	StatusActive   = "active"
	StatusError    = "error"
)

// Info is the admin-facing view of one discovered plugin.
type Info struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	APIVersion   int      `json:"api_version"`
	Capabilities []string `json:"capabilities"`
	Description  string   `json:"description,omitempty"`
	Enabled      bool     `json:"enabled"`
	Status       string   `json:"status"`
	Error        string   `json:"error,omitempty"`
}

// Manager discovers plugins under a directory, enables/disables them, and
// invokes declared capabilities through an injected Loader.
type Manager struct {
	dir    string
	loader Loader
	limits Limits
	state  *enableStore

	mu      sync.Mutex
	modules map[string]Module
	lastErr map[string]string
}

// Options configures a Manager.
type Options struct {
	Dir    string
	Loader Loader
	Limits Limits
	// StatePath defaults to Dir/enabled.json when empty.
	StatePath string
}

// NewManager builds a Manager. An empty Dir is valid: List returns nothing and
// music playback is unaffected. Loader defaults to WazeroLoader.
func NewManager(opts Options) (*Manager, error) {
	dir := strings.TrimSpace(opts.Dir)
	loader := opts.Loader
	if loader == nil {
		loader = NewWazeroLoader()
	}
	limits := opts.Limits.withDefaults()
	statePath := opts.StatePath
	if statePath == "" && dir != "" {
		statePath = filepath.Join(dir, "enabled.json")
	}
	var store *enableStore
	if statePath != "" {
		var err error
		store, err = newEnableStore(statePath)
		if err != nil {
			return nil, err
		}
	} else {
		store = &enableStore{ids: map[string]bool{}}
	}
	return &Manager{
		dir:     dir,
		loader:  loader,
		limits:  limits,
		state:   store,
		modules: map[string]Module{},
		lastErr: map[string]string{},
	}, nil
}

// Close releases every loaded module.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var first error
	for id, mod := range m.modules {
		if err := mod.Close(ctx); err != nil && first == nil {
			first = err
		}
		delete(m.modules, id)
	}
	return first
}

// List returns discovered plugins with enable/status fields.
func (m *Manager) List(ctx context.Context) ([]Info, error) {
	_ = ctx
	discovered, err := m.discover()
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(discovered))
	for _, d := range discovered {
		out = append(out, m.infoFor(d))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Get returns one plugin by id.
func (m *Manager) Get(ctx context.Context, id string) (Info, error) {
	_ = ctx
	d, err := m.discoverOne(id)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return Info{}, err
		}
		return m.infoFor(d), nil
	}
	return m.infoFor(d), nil
}

// Enable loads the plugin WASM and marks it enabled.
func (m *Manager) Enable(ctx context.Context, id string) (Info, error) {
	d, err := m.discoverOne(id)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return Info{}, err
		}
		return m.infoFor(d), err
	}
	if err := m.load(ctx, d); err != nil {
		m.setErr(d.manifest.ID, err.Error())
		_ = m.state.set(d.manifest.ID, false)
		return m.infoFor(d), err
	}
	if err := m.state.set(d.manifest.ID, true); err != nil {
		return Info{}, err
	}
	m.setErr(d.manifest.ID, "")
	return m.infoFor(d), nil
}

// Disable unloads the module and marks it disabled. Core playback is unrelated.
func (m *Manager) Disable(ctx context.Context, id string) (Info, error) {
	d, err := m.discoverOne(id)
	if errors.Is(err, errNotFound) {
		return Info{}, err
	}
	pluginID := d.manifest.ID
	if pluginID == "" {
		pluginID = strings.TrimSpace(strings.ToLower(id))
	}
	m.unload(ctx, pluginID)
	if err := m.state.set(pluginID, false); err != nil {
		return Info{}, err
	}
	m.setErr(pluginID, "")
	return m.infoFor(d), nil
}

// Invoke runs a declared capability on an enabled plugin.
func (m *Manager) Invoke(ctx context.Context, id, capability string) (uint64, error) {
	capability = strings.TrimSpace(capability)
	d, err := m.discoverOne(id)
	if err != nil {
		return 0, err
	}
	if !m.state.enabled(id) {
		return 0, errDisabled
	}
	ok := false
	for _, c := range d.manifest.Capabilities {
		if c == capability {
			ok = true
			break
		}
	}
	if !ok {
		return 0, fmt.Errorf("%w: %s", errUnknownCapability, capability)
	}
	m.mu.Lock()
	mod := m.modules[id]
	m.mu.Unlock()
	if mod == nil {
		if err := m.load(ctx, d); err != nil {
			m.setErr(id, err.Error())
			return 0, err
		}
		m.mu.Lock()
		mod = m.modules[id]
		m.mu.Unlock()
	}
	callCtx, cancel := context.WithTimeout(ctx, m.limits.CallTimeout)
	defer cancel()
	res, err := mod.Call(callCtx, capability)
	if err != nil {
		m.setErr(id, err.Error())
		return 0, err
	}
	m.setErr(id, "")
	return res, nil
}

var (
	errNotFound          = errors.New("plugin not found")
	errDisabled          = errors.New("plugin is disabled")
	errUnknownCapability = errors.New("capability not declared")
)

// ErrNotFound reports a missing plugin id.
func ErrNotFound() error { return errNotFound }

// ErrDisabled reports an invoke against a disabled plugin.
func ErrDisabled() error { return errDisabled }

type discovered struct {
	dir      string
	manifest Manifest
	wasmPath string
}

func (m *Manager) discover() ([]discovered, error) {
	if m.dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read plugin dir: %w", err)
	}
	var out []discovered
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		d, err := m.readPluginDir(filepath.Join(m.dir, e.Name()), e.Name())
		if err != nil {
			// Invalid manifests appear as error-status rows so admins see them.
			out = append(out, discovered{
				dir: filepath.Join(m.dir, e.Name()),
				manifest: Manifest{
					ID:         strings.ToLower(e.Name()),
					Name:       e.Name(),
					Version:    "?",
					APIVersion: 0,
					Entry:      "plugin.wasm",
				},
			})
			m.setErr(strings.ToLower(e.Name()), err.Error())
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

func (m *Manager) discoverOne(id string) (discovered, error) {
	id = strings.TrimSpace(strings.ToLower(id))
	if id == "" || !validPluginID(id) {
		return discovered{}, errNotFound
	}
	if m.dir == "" {
		return discovered{}, errNotFound
	}
	dir := filepath.Join(m.dir, id)
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return discovered{}, errNotFound
	}
	d, err := m.readPluginDir(dir, id)
	if err != nil {
		m.setErr(id, err.Error())
		return discovered{
			dir: dir,
			manifest: Manifest{
				ID: id, Name: id, Version: "?", Entry: "plugin.wasm",
			},
		}, err
	}
	return d, nil
}

func (m *Manager) readPluginDir(dir, fallbackID string) (discovered, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	f, err := os.Open(manifestPath)
	if err != nil {
		return discovered{}, err
	}
	defer f.Close()
	man, err := ParseManifest(f)
	if err != nil {
		return discovered{}, err
	}
	dirID := strings.ToLower(filepath.Base(dir))
	if man.ID != "" && man.ID != dirID {
		return discovered{}, fmt.Errorf("manifest id %q must match directory %q", man.ID, dirID)
	}
	man.ID = dirID
	if fallbackID != "" && man.ID != fallbackID {
		return discovered{}, fmt.Errorf("manifest id %q must match directory %q", man.ID, fallbackID)
	}
	wasmPath := filepath.Join(dir, man.Entry)
	st, err := os.Stat(wasmPath)
	if err != nil {
		return discovered{}, fmt.Errorf("plugin entry: %w", err)
	}
	if st.IsDir() {
		return discovered{}, errors.New("plugin entry is a directory")
	}
	if st.Size() > m.limits.MaxWasmBytes {
		return discovered{}, fmt.Errorf("plugin wasm exceeds %d bytes", m.limits.MaxWasmBytes)
	}
	return discovered{dir: dir, manifest: man, wasmPath: wasmPath}, nil
}

func (m *Manager) infoFor(d discovered) Info {
	id := d.manifest.ID
	enabled := m.state.enabled(id)
	info := Info{
		ID:           id,
		Name:         d.manifest.Name,
		Version:      d.manifest.Version,
		APIVersion:   d.manifest.APIVersion,
		Capabilities: append([]string(nil), d.manifest.Capabilities...),
		Description:  d.manifest.Description,
		Enabled:      enabled,
		Status:       StatusDisabled,
	}
	m.mu.Lock()
	errMsg := m.lastErr[id]
	_, loaded := m.modules[id]
	m.mu.Unlock()
	if errMsg != "" {
		info.Status = StatusError
		info.Error = errMsg
		return info
	}
	if enabled && loaded {
		info.Status = StatusActive
	}
	return info
}

func (m *Manager) load(ctx context.Context, d discovered) error {
	if d.wasmPath == "" || d.manifest.APIVersion != CurrentAPIVersion {
		if d.manifest.APIVersion != CurrentAPIVersion && d.manifest.APIVersion != 0 {
			return fmt.Errorf("unsupported api_version %d (want %d)", d.manifest.APIVersion, CurrentAPIVersion)
		}
		if d.wasmPath == "" {
			return errors.New("plugin is not loadable")
		}
	}
	wasm, err := os.ReadFile(d.wasmPath)
	if err != nil {
		return fmt.Errorf("read wasm: %w", err)
	}
	if int64(len(wasm)) > m.limits.MaxWasmBytes {
		return fmt.Errorf("plugin wasm exceeds %d bytes", m.limits.MaxWasmBytes)
	}
	loadCtx, cancel := context.WithTimeout(ctx, m.limits.CallTimeout)
	defer cancel()
	mod, err := m.loader.Instantiate(loadCtx, wasm, m.limits)
	if err != nil {
		return err
	}
	for _, capName := range d.manifest.Capabilities {
		probeCtx, probeCancel := context.WithTimeout(ctx, 500*time.Millisecond)
		_, callErr := mod.Call(probeCtx, capName)
		probeCancel()
		if callErr != nil && !errors.Is(callErr, context.DeadlineExceeded) {
			// Missing export is a hard failure; timeout during enable still fails.
			_ = mod.Close(ctx)
			return fmt.Errorf("capability %q: %w", capName, callErr)
		}
		if callErr != nil {
			_ = mod.Close(ctx)
			return fmt.Errorf("capability %q: %w", capName, callErr)
		}
	}
	m.mu.Lock()
	if old := m.modules[d.manifest.ID]; old != nil {
		_ = old.Close(ctx)
	}
	m.modules[d.manifest.ID] = mod
	m.mu.Unlock()
	return nil
}

func (m *Manager) unload(ctx context.Context, id string) {
	m.mu.Lock()
	mod := m.modules[id]
	delete(m.modules, id)
	m.mu.Unlock()
	if mod != nil {
		_ = mod.Close(ctx)
	}
}

func (m *Manager) setErr(id, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg == "" {
		delete(m.lastErr, id)
		return
	}
	m.lastErr[id] = msg
}
