package service

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

func handleWorkerPanic(logger *slog.Logger, worker string, r any) error {
	if r == nil {
		return nil
	}
	err := fmt.Errorf("%s panic: %v", worker, r)
	if logger != nil {
		logger.Error("background worker panic", "worker", worker, "panic", r, "stack", string(debug.Stack()))
	}
	return err
}
