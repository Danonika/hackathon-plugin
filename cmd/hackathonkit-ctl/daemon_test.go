package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"hackathonkit.local/agentkit/internal/control"
)

func testControlStore(t *testing.T) *control.Store {
	t.Helper()
	store, err := control.Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.UpsertSession(context.Background(), control.Session{ID: "s-1", Status: "active", Task: "demo"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(context.Background(), control.Event{SessionID: "s-1", Type: "tool_use", Tool: "system.context"}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestControlHandlerEndpoints(t *testing.T) {
	store := testControlStore(t)
	handler := controlHandler(store, "")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", rec.Code)
	}
	var health map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health["sessions"].(float64) != 1 {
		t.Fatalf("healthz sessions = %v", health["sessions"])
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions status = %d", rec.Code)
	}
	var list struct {
		Sessions []control.Session `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) != 1 || list.Sessions[0].ID != "s-1" {
		t.Fatalf("sessions = %+v", list.Sessions)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/s-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("session detail status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing session status = %d, want 404", rec.Code)
	}
}

func TestControlHandlerToken(t *testing.T) {
	store := testControlStore(t)
	handler := controlHandler(store, "secret")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("without token status = %d, want 401", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("X-HackathonKit-Token", "secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with token status = %d, want 200", rec.Code)
	}
}

func TestControlHandlerRejectsCrossOriginMutationsWithoutToken(t *testing.T) {
	store := testControlStore(t)
	handler := controlHandler(store, "")

	req := httptest.NewRequest(http.MethodPost, "/api/risk", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST status = %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/risk", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("local POST status = %d, want 200", rec.Code)
	}
}
