package service

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// RecoverWorker is the unified panic-recovery helper for background workers.
// Pass the value returned by recover() in r; when non-nil it logs the panic
// together with the stack trace and returns a formatted error so callers can
// propagate the failure (e.g. to scan progress) if they care. When r is nil
// the function is a no-op, which makes it safe to use unconditionally inside
// a deferred wrapper.
//
// Typical usage:
//
//	defer func() {
//	    service.RecoverWorker(logger, "podcast checker", recover())
//	}()
//
// This consolidates what used to be duplicated as handleWorkerPanic (here)
// and recoverBackgroundWorkerPanic (in cmd/player) into one exported variant.
func RecoverWorker(logger *slog.Logger, worker string, r any) error {
	if r == nil {
		return nil
	}
	err := fmt.Errorf("%s panic: %v", worker, r)
	if logger != nil {
		logger.Error("background worker panic", "worker", worker, "panic", r, "stack", string(debug.Stack()))
	}
	return err
}
