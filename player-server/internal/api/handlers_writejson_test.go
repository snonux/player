package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWriteJSON_Success covers the happy path: a value that encodes cleanly
// should reach the client with the caller-supplied status and the expected
// Content-Type header. This guards against accidental regressions in the
// buffered-encode path (e.g. forgetting to write buf.Bytes()).
func TestWriteJSON_Success(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSON(rr, http.StatusCreated, map[string]string{"hello": "world"})

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
	var decoded map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("body is not valid JSON: %v (body=%q)", err, rr.Body.String())
	}
	if decoded["hello"] != "world" {
		t.Fatalf("expected hello=world, got %v", decoded)
	}
}

// TestWriteJSON_EncodeFailureYields500 is the regression test for the bug
// fixed in y9: previously writeJSON wrote the status header BEFORE attempting
// to encode the body, so a value that json cannot marshal (channels, funcs)
// produced a 200 with a corrupt body. After the refactor we marshal into a
// bytes.Buffer first; if that fails we MUST emit 500 plus a fallback JSON
// error payload instead of the caller's success status.
func TestWriteJSON_EncodeFailureYields500(t *testing.T) {
	rr := httptest.NewRecorder()
	// chan int is not marshalable by encoding/json; this forces the failure
	// branch without needing a custom MarshalJSON implementation.
	unmarshalable := make(chan int)

	writeJSON(rr, http.StatusOK, unmarshalable)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d on encode failure, got %d", http.StatusInternalServerError, rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
	body := rr.Body.String()
	// Sanity: body must be valid JSON and look like an error envelope.
	if !strings.Contains(body, `"error"`) {
		t.Fatalf("expected fallback error body to contain \"error\" key, got %q", body)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("fallback body is not valid JSON: %v (body=%q)", err, body)
	}
	if decoded["error"] == "" {
		t.Fatalf("expected non-empty error field, got %v", decoded)
	}
}
