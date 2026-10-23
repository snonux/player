package plugin

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// WazeroLoader is the production WASM Loader backed by wazero.
type WazeroLoader struct{}

// NewWazeroLoader returns a Loader that instantiates guest modules with
// CloseOnContextDone so CallTimeout can abort runaway guests.
func NewWazeroLoader() *WazeroLoader {
	return &WazeroLoader{}
}

var _ Loader = (*WazeroLoader)(nil)

// Instantiate compiles wasm and creates one module instance.
func (WazeroLoader) Instantiate(ctx context.Context, wasm []byte, limits Limits) (Module, error) {
	limits = limits.withDefaults()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(limits.MaxMemoryPages))
	compiled, err := rt.CompileModule(ctx, wasm)
	if err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("compile wasm: %w", err)
	}
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithStartFunctions())
	if err != nil {
		_ = compiled.Close(ctx)
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("instantiate wasm: %w", err)
	}
	return &wazeroModule{rt: rt, compiled: compiled, mod: mod}, nil
}

type wazeroModule struct {
	rt       wazero.Runtime
	compiled wazero.CompiledModule
	mod      api.Module
}

func (m *wazeroModule) Call(ctx context.Context, export string) (uint64, error) {
	fn := m.mod.ExportedFunction(export)
	if fn == nil {
		return 0, fmt.Errorf("export %q not found", export)
	}
	results, err := fn.Call(ctx)
	if err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, nil
	}
	return results[0], nil
}

func (m *wazeroModule) Close(ctx context.Context) error {
	var first error
	if err := m.mod.Close(ctx); err != nil {
		first = err
	}
	if err := m.compiled.Close(ctx); err != nil && first == nil {
		first = err
	}
	if err := m.rt.Close(ctx); err != nil && first == nil {
		first = err
	}
	return first
}
