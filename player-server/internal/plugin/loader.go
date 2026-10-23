package plugin

import "context"

// Module is one loaded WASM instance. Implementations must be safe for one
// Call at a time; the Manager serialises per-plugin use.
type Module interface {
	// Call invokes an exported function that returns a single i32.
	Call(ctx context.Context, export string) (uint64, error)
	// Close releases the instance.
	Close(ctx context.Context) error
}

// Loader compiles and instantiates WASM bytecode under the given limits.
// Injected so tests can substitute a fake without pulling wazero.
type Loader interface {
	Instantiate(ctx context.Context, wasm []byte, limits Limits) (Module, error)
}
