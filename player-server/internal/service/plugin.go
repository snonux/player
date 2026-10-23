package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/snonux/player/internal/plugin"
)

// ErrPluginDisabled is returned when invoking a disabled plugin.
var ErrPluginDisabled = &apiError{msg: "plugin is disabled", status: http.StatusConflict}

// ErrPluginInvalid is returned for manifest/runtime contract failures.
var ErrPluginInvalid = &apiError{msg: "plugin is invalid", status: http.StatusBadRequest}

// PluginService is the admin-facing plugin runtime boundary.
type PluginService interface {
	List(ctx context.Context) ([]plugin.Info, error)
	Get(ctx context.Context, id string) (plugin.Info, error)
	Enable(ctx context.Context, id string) (plugin.Info, error)
	Disable(ctx context.Context, id string) (plugin.Info, error)
	Invoke(ctx context.Context, id, capability string) (PluginInvokeResult, error)
}

// PluginInvokeResult is the JSON body for a successful capability call.
type PluginInvokeResult struct {
	ID         string `json:"id"`
	Capability string `json:"capability"`
	Result     uint64 `json:"result"`
}

// pluginService adapts plugin.Manager to PluginService.
type pluginService struct {
	mgr *plugin.Manager
}

// NewPluginService wraps a Manager. mgr must be non-nil.
func NewPluginService(mgr *plugin.Manager) PluginService {
	return &pluginService{mgr: mgr}
}

var _ PluginService = (*pluginService)(nil)

func (s *pluginService) List(ctx context.Context) ([]plugin.Info, error) {
	return s.mgr.List(ctx)
}

func (s *pluginService) Get(ctx context.Context, id string) (plugin.Info, error) {
	info, err := s.mgr.Get(ctx, id)
	if err != nil {
		return plugin.Info{}, mapPluginError(err)
	}
	return info, nil
}

func (s *pluginService) Enable(ctx context.Context, id string) (plugin.Info, error) {
	info, err := s.mgr.Enable(ctx, id)
	if err != nil {
		return info, mapPluginError(err)
	}
	return info, nil
}

func (s *pluginService) Disable(ctx context.Context, id string) (plugin.Info, error) {
	info, err := s.mgr.Disable(ctx, id)
	if err != nil {
		return plugin.Info{}, mapPluginError(err)
	}
	return info, nil
}

func (s *pluginService) Invoke(ctx context.Context, id, capability string) (PluginInvokeResult, error) {
	res, err := s.mgr.Invoke(ctx, id, capability)
	if err != nil {
		return PluginInvokeResult{}, mapPluginError(err)
	}
	return PluginInvokeResult{ID: id, Capability: capability, Result: res}, nil
}

func mapPluginError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, plugin.ErrNotFound()):
		return ErrNotFound
	case errors.Is(err, plugin.ErrDisabled()):
		return ErrPluginDisabled
	case strings.Contains(err.Error(), "api_version"),
		strings.Contains(err.Error(), "unknown capability"),
		strings.Contains(err.Error(), "capability"),
		strings.Contains(err.Error(), "manifest"),
		strings.Contains(err.Error(), "export"):
		return &apiError{msg: err.Error(), status: http.StatusBadRequest}
	default:
		// Timeouts and instantiate failures stay informative 400s for admins.
		return &apiError{msg: err.Error(), status: http.StatusBadRequest}
	}
}
