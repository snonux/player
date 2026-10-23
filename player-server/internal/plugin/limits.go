package plugin

import "time"

// Limits bounds a single WASM instance. Zero fields use Defaults().
type Limits struct {
	// MaxWasmBytes is the largest plugin.wasm accepted at load time.
	MaxWasmBytes int64
	// MaxMemoryPages is the WASM linear-memory page cap (64 KiB each).
	MaxMemoryPages uint32
	// CallTimeout is the maximum wall time for one capability call.
	CallTimeout time.Duration
}

// Defaults returns conservative A08a resource limits.
func Defaults() Limits {
	return Limits{
		MaxWasmBytes:   2 << 20, // 2 MiB
		MaxMemoryPages: 256,     // 16 MiB
		CallTimeout:    2 * time.Second,
	}
}

func (l Limits) withDefaults() Limits {
	d := Defaults()
	if l.MaxWasmBytes <= 0 {
		l.MaxWasmBytes = d.MaxWasmBytes
	}
	if l.MaxMemoryPages == 0 {
		l.MaxMemoryPages = d.MaxMemoryPages
	}
	if l.CallTimeout <= 0 {
		l.CallTimeout = d.CallTimeout
	}
	return l
}
