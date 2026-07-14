package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"auth"
)

func TestGreetingHandler(t *testing.T) {
	tests := []struct {
		name           string
		user           *auth.User // nil means no user in context
		wantStatus     int
		wantMessage    string // if non‑empty, we expect this message in the JSON response
		wantHeaderWWW  bool   // expect WWW‑Authenticate header on 401
	}{
		{
			name:          "no auth",
			user:          nil,
			wantStatus:    http.StatusUnauthorized,
			wantHeaderWWW: true,
		},
		{
			name:          "empty name",
			user:          &auth.User{ID: "123", Name: ""},
			wantStatus:    http.StatusBadRequest,
			wantMessage:   "", // not needed
			wantHeaderWWW: false,
		},
		{
			name:          "whitespace name",
			user:          &auth.User{ID: "123", Name: "   \t\n"},
			wantStatus:    http.StatusBadRequest,
			wantHeaderWWW: false,
		},
		{
			name:          "valid name",
			user:          &auth.User{ID: "123", Name: "Alice"},
			wantStatus:    http.StatusOK,
			wantMessage:   "hello Alice",
			wantHeaderWWW: false,
		},
		{
			name:          "name with control chars",
			user:          &auth.User{ID: "123", Name: "Bob\x00\x01"},
			wantStatus:    http.StatusOK,
			wantMessage:   "hello Bob",
			wantHeaderWWW: false,
		},
		{
			name:          "name exceeding limit",
			user:          &auth.User{ID: "123", Name: generateLongName(150)},
			wantStatus:    http.StatusOK,
			wantMessage:   "hello " + generateLongName(100), // truncated at 100 runes
			wantHeaderWWW: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/greeting", nil)

			// Inject user into context if provided.
			if tt.user != nil {
				req = req.WithContext(auth.NewContextWithUser(req.Context(), tt.user))
			}

			rr := httptest.NewRecorder()
			GreetingHandler(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}

			if tt.wantHeaderWWW {
				if h := rr.Header().Get("WWW-Authenticate"); h == "" {
					t.Fatalf("expected WWW-Authenticate header, got none")
				}
			}

			// Validate JSON body for successful cases.
			if tt.wantStatus == http.StatusOK {
				var resp greetingResp
				if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode JSON response: %v", err)
				}
				if resp.Message != tt.wantMessage {
					t.Fatalf("expected message %q, got %q", tt.wantMessage, resp.Message)
				}
				// Check security headers.
				if got := rr.Header().Get("Cache-Control"); got != "no-store, private" {
					t.Fatalf("expected Cache-Control header, got %q", got)
				}
				if got := rr.Header().Get("Vary"); got != "Authorization" {
					t.Fatalf("expected Vary header, got %q", got)
				}
				if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
					t.Fatalf("expected X-Content-Type-Options header, got %q", got)
				}
			} else {
				// For error cases, ensure the response is JSON with an "error" field.
				var errResp map[string]interface{}
				bodyBytes := rr.Body.Bytes()
				if len(bytes.TrimSpace(bodyBytes)) == 0 {
					t.Fatalf("expected non‑empty error body")
				}
				if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&errResp); err != nil {
					t.Fatalf("error body not valid JSON: %v", err)
				}
				if _, ok := errResp["error"]; !ok {
					t.Fatalf("error JSON missing \"error\" field")
				}
			}
		})
	}
}

// generateLongName returns a string consisting of `length` repetitions of "x".
func generateLongName(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}