package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDebugHealthEndpoint(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/debug/health", nil)

	(&Server{healthCheck: func(context.Context) error { return nil }}).handleDebugHealth(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if payload["status"] != "UP" {
		t.Fatalf("payload = %#v, want status UP", payload)
	}
}

func TestDebugHealthEndpointDown(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/debug/health", nil)

	(&Server{healthCheck: func(context.Context) error { return errors.New("unreachable") }}).handleDebugHealth(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if payload["status"] != "DOWN" {
		t.Fatalf("payload = %#v, want status DOWN", payload)
	}
	if payload["error"] == "" {
		t.Fatalf("payload = %#v, want error message", payload)
	}
}
