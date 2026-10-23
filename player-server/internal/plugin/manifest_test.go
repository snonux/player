package plugin

import (
	"strings"
	"testing"
)

func TestParseManifest_OK(t *testing.T) {
	m, err := ParseManifest(strings.NewReader(`{
		"id": "ok-plugin",
		"name": "OK",
		"version": "1.0.0",
		"api_version": 1,
		"capabilities": ["probe"],
		"entry": "plugin.wasm"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "ok-plugin" || m.APIVersion != CurrentAPIVersion {
		t.Fatalf("unexpected manifest: %+v", m)
	}
}

func TestParseManifest_RejectsUnknownAPIVersion(t *testing.T) {
	_, err := ParseManifest(strings.NewReader(`{
		"id": "bad-version",
		"name": "Bad",
		"version": "1.0.0",
		"api_version": 99,
		"capabilities": ["probe"],
		"entry": "plugin.wasm"
	}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported api_version") {
		t.Fatalf("want api_version error, got %v", err)
	}
}

func TestParseManifest_RejectsUnknownCapability(t *testing.T) {
	_, err := ParseManifest(strings.NewReader(`{
		"id": "x",
		"name": "X",
		"version": "1",
		"api_version": 1,
		"capabilities": ["not-a-real-cap"],
		"entry": "plugin.wasm"
	}`))
	if err == nil || !strings.Contains(err.Error(), "unknown capability") {
		t.Fatalf("want capability error, got %v", err)
	}
}

func TestParseManifest_RejectsPathEntry(t *testing.T) {
	_, err := ParseManifest(strings.NewReader(`{
		"id": "x",
		"name": "X",
		"version": "1",
		"api_version": 1,
		"capabilities": ["probe"],
		"entry": "../escape.wasm"
	}`))
	if err == nil {
		t.Fatal("expected entry path rejection")
	}
}
