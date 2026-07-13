package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/project/auth"
)

func TestGreetingHandler_Unauthenticated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/greeting", nil)
	rr := httptest.NewRecorder()

	GreetingHandler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
	if got := rr.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatalf("expected WWW-Authenticate header to be set")
	}
}

func TestGreetingHandler_EmptyName(t *testing.T) {
	user := &auth.User{ID: "123", Name: ""}
	ctx := auth.NewContext(reqContext(), user)
	req := httptest.NewRequest(http.MethodGet, "/greeting", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	GreetingHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rr.Code)
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := payload["error"]; !ok {
		t.Fatalf("expected error field in response")
	}
}

func TestGreetingHandler_ValidName(t *testing.T) {
	user := &auth.User{ID: "u1", Name: "Alice"}
	ctx := auth.NewContext(reqContext(), user)
	req := httptest.NewRequest(http.MethodGet, "/greeting", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	GreetingHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var resp greetingResp
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	expected := "hello Alice"
	if resp.Message != expected {
		t.Fatalf("expected message %q, got %q", expected, resp.Message)
	}
	// Security headers.
	if got := rr.Header().Get("Cache-Control"); got != "no-store, private" {
		t.Fatalf("Cache-Control header mismatch: %s", got)
	}
	if got := rr.Header().Get("Vary"); got != "Authorization" {
		t.Fatalf("Vary header mismatch: %s", got)
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options header mismatch: %s", got)
	}
}

// Helper that returns a non‑nil background context.
func reqContext() context.Context {
	return context.Background()
}

func TestGreetingHandler_NameSanitisation(t *testing.T) {
	original := "\tJohn\x00Doe\x7F"
	user := &auth.User{ID: "u2", Name: original}
	ctx := auth.NewContext(reqContext(), user)
	req := httptest.NewRequest(http.MethodGet, "/greeting", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	GreetingHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	var resp greetingResp
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	// Control characters should be stripped.
	expected := "hello JohnDoe"
	if resp.Message != expected {
		t.Fatalf("expected %q, got %q", expected, resp.Message)
	}
}

func TestGreetingHandler_MethodNotAllowed(t *testing.T) {
	user := &auth.User{ID: "u3", Name: "Bob"}
	ctx := auth.NewContext(reqContext(), user)
	req := httptest.NewRequest(http.MethodPost, "/greeting", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	GreetingHandler(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected %d for non‑GET, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}