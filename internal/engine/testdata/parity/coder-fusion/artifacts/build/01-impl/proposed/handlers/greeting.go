package handlers

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/example/project/auth"
)

const greetingTemplate = "hello %s"

type greetingResp struct {
	Message string `json:"message"`
}

// GreetingHandler returns a personalised greeting for an authenticated user.
// It validates the user's name, sanitises it, and emits security‑related
// response headers.
func GreetingHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Context cancellation check.
	select {
	case <-r.Context().Done():
		// The request has been cancelled or timed out.
		writeError(w, r.Context().Err(), http.StatusRequestTimeout)
		return
	default:
		// continue
	}

	// 2. Extract the user from the request context.
	u, err := auth.UserFromContext(r.Context())
	if err != nil {
		if errors.Is(err, auth.ErrUnauthenticated) {
			if w.Header().Get("WWW-Authenticate") == "" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
			}
			writeError(w, err, http.StatusUnauthorized)
			return
		}
		writeError(w, err, http.StatusInternalServerError)
		return
	}

	// 3. Validate and sanitise the user's name.
	name := strings.TrimSpace(u.Name)
	if name == "" {
		writeError(w, errors.New("user name not set"), http.StatusBadRequest)
		return
	}

	// Remove ASCII control characters and limit to 100 runes.
	var sb strings.Builder
	runeCount := 0
	for _, r := range name {
		// Skip control characters (U+0000‑U+001F and U+007F).
		if r >= 0x20 && r != 0x7F {
			sb.WriteRune(r)
			runeCount++
			if runeCount >= 100 {
				break
			}
		}
	}
	sanitized := sb.String()

	msg := fmt.Sprintf(greetingTemplate, sanitized)

	// 4. Set security‑related headers.
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Vary", "Authorization")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// 5. Log the request (structured with only the user ID).
	log.Printf("user_id=%s", u.ID)

	// 6. Write the JSON response.
	writeJSON(w, greetingResp{Message: msg}, http.StatusOK)
}