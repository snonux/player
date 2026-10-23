package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/plugin"
	"github.com/snonux/player/internal/service"
)

func TestPluginAdminAPI_EnableInvokeDisable(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "ok-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join("..", "plugin", "testdata", "ok-plugin")
	for _, name := range []string{"manifest.json", "plugin.wasm"} {
		b, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pluginDir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mgr, err := plugin.NewManager(plugin.Options{
		Dir:       dir,
		Loader:    plugin.NewWazeroLoader(),
		StatePath: filepath.Join(t.TempDir(), "enabled.json"),
		Limits:    plugin.Limits{CallTimeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close(context.Background())
	srv := &Server{pluginSvc: service.NewPluginService(mgr)}

	list := httptest.NewRecorder()
	srv.handleListPlugins(list, httptest.NewRequest(http.MethodGet, "/api/admin/plugins", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list %d %s", list.Code, list.Body)
	}
	var items []plugin.Info
	if err := json.Unmarshal(list.Body.Bytes(), &items); err != nil || len(items) != 1 {
		t.Fatalf("list body: %v %#v", err, items)
	}

	enable := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/plugins/ok-plugin/enable", nil)
	req.SetPathValue("id", "ok-plugin")
	srv.handleEnablePlugin(enable, req)
	if enable.Code != http.StatusOK {
		t.Fatalf("enable %d %s", enable.Code, enable.Body)
	}

	invoke := httptest.NewRecorder()
	ireq := httptest.NewRequest(http.MethodPost, "/api/admin/plugins/ok-plugin/invoke", strings.NewReader(`{"capability":"probe"}`))
	ireq.SetPathValue("id", "ok-plugin")
	srv.handleInvokePlugin(invoke, ireq)
	if invoke.Code != http.StatusOK || !strings.Contains(invoke.Body.String(), `"result":42`) {
		t.Fatalf("invoke %d %s", invoke.Code, invoke.Body)
	}

	disable := httptest.NewRecorder()
	dreq := httptest.NewRequest(http.MethodPost, "/api/admin/plugins/ok-plugin/disable", nil)
	dreq.SetPathValue("id", "ok-plugin")
	srv.handleDisablePlugin(disable, dreq)
	if disable.Code != http.StatusOK {
		t.Fatalf("disable %d %s", disable.Code, disable.Body)
	}

	denied := httptest.NewRecorder()
	dinv := httptest.NewRequest(http.MethodPost, "/api/admin/plugins/ok-plugin/invoke", strings.NewReader(`{"capability":"probe"}`))
	dinv.SetPathValue("id", "ok-plugin")
	srv.handleInvokePlugin(denied, dinv)
	if denied.Code != http.StatusConflict {
		t.Fatalf("disabled invoke %d %s", denied.Code, denied.Body)
	}
}

func TestPluginAdminAPI_NilService(t *testing.T) {
	srv := &Server{}
	rec := httptest.NewRecorder()
	srv.handleListPlugins(rec, httptest.NewRequest(http.MethodGet, "/api/admin/plugins", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("got %d", rec.Code)
	}
}
