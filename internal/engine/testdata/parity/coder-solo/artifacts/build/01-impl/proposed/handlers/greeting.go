package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"example.com/project/auth"
)

// greetingResp is the JSON envelope returned by GreetingHandler.
type greetingResp struct {
	Message string `json:"message"`
}

// greetingTemplate defines the format of the greeting string.
const greetingTemplate = "hello %s"

const maxNameRunes = 100

// GreetingHandler returns a personalised greeting for an authenticated user.
// It performs the following steps:
//   1. Rejects non‑GET methods.
//   2. Checks for request cancellation.
//   3. Retrieves the authenticated user via auth.UserFromContext.
//   4. Validates and sanitises the user's name.
//   5. Writes security headers.
//   6. Returns a JSON payload containing the greeting.
func GreetingHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Ensure this endpoint is only reachable via GET.
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// 2. Respect request cancellation.
	if err := r.Context().Err(); err != nil {
		writeError(w, err, http.StatusRequestTimeout)
		return
	}

	// 3. Extract user from context.
	u, err := auth.UserFromContext(r.Context())
	if err != nil {
		// Unauthenticated – ensure WWW‑Authenticate header is present.
		if errors.Is(err, auth.ErrUnauthenticated) {
			if w.Header().Get("WWW-Authenticate") == "" {
				// The realm is consistent with existing auth middleware.
				w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
			}
			writeError(w, err, http.StatusUnauthorized)
			return
		}
		// Any other error → internal server error.
		writeError(w, err, http.StatusInternalServerError)
		return
	}

	// 4. Validate and sanitise the name.
	name := strings.TrimSpace(u.Name)
	if name == "" {
		writeError(w, errors.New("user name not set"), http.StatusBadRequest)
		return
	}
	// Remove control characters and other non‑printable runes.
	var sb strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) {
			continue
		}
		// Disallow the DEL character (0x7F) explicitly.
		if r == 0x7F {
			continue
		}
		sb.WriteRune(r)
	}
	clean := sb.String()

	// Truncate to the maximum allowed length (in runes).
	runes := []rune(clean)
	if len(runes) > maxNameRunes {
		runes = runes[:maxNameRunes]
	}
	finalName := string(runes)

	// 5. Set security‑related headers.
	h := w.Header()
	h.Set("Cache-Control", "no-store, private")
	h.Set("Vary", "Authorization")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Type", "application/json")

	// 6. Build greeting and encode response.
	msg := fmt.Sprintf(greetingTemplate, finalName)
	resp := greetingResp{Message: msg}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// If encoding fails, fall back to a generic error response.
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	// Explicitly set the success status (Encode does not set it).
	w.WriteHeader(http.StatusOK)

	// Log the request – only user ID is logged to avoid leaking PII.
	// The project already uses the standard logger; adjust if a structured
	// logger is in use elsewhere.
	fmt.Printf("greeting request served for user_id=%s\n", u.ID)
}