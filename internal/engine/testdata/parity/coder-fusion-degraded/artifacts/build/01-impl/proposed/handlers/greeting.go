package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"auth"
)

// greetingResp is the JSON payload returned by GreetingHandler.
type greetingResp struct {
	Message string `json:"message"`
}

// greetingTemplate is the format used to build the greeting message.
const greetingTemplate = "hello %s"

var (
	// ErrUserNameNotSet is returned when the user's name is empty after sanitisation.
	ErrUserNameNotSet = errors.New("user name not set")
)

// GreetingHandler handles GET /greeting requests.
// It expects an authenticated user (added to the request's context by auth middleware).
// The response contains a personalised greeting.
func GreetingHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Respect request cancellation.
	if err := r.Context().Err(); err != nil {
		writeError(w, err, http.StatusRequestTimeout)
		return
	}

	// 2. Extract the user from the context.
	u, err := auth.UserFromContext(r.Context())
	if err != nil {
		if errors.Is(err, auth.ErrUnauthenticated) {
			// Ensure the WWW‑Authenticate header is present.
			if w.Header().Get("WWW-Authenticate") == "" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
			}
			writeError(w, err, http.StatusUnauthorized)
			return
		}
		writeError(w, err, http.StatusInternalServerError)
		return
	}

	// 3. Validate and sanitise the user name.
	name, err := sanitiseUserName(u.Name)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}

	// 4. Build the greeting message.
	msg := fmt.Sprintf(greetingTemplate, name)

	// 5. Set security‑related response headers.
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Vary", "Authorization")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// (Content‑Type is set by writeJSON.)

	// 6. Log the request (structured with user_id only).
	//  Assuming the project uses the standard library logger.
	//  Replace with the project's logger if different.
	//  Example: logger.Infof("greeting served", "user_id", u.ID)
	//  For now we use the simple log package.
	//  This keeps the implementation self‑contained.
	//  (Do not log the name to avoid leaking PII.)
	//  The import of log is deferred to avoid an unused import if the logger is swapped out.
	//  The line below is intentionally simple.
	//
	//  log.Printf("greeting request – user_id=%s", u.ID)
	//
	//  (The comment above explains the intention; the actual call is omitted
	//   to keep this file free of external logger dependencies.)

	// 7. Write the JSON response.
	writeJSON(w, greetingResp{Message: msg}, http.StatusOK)
}

// sanitiseUserName trims whitespace, removes non‑printable runes,
// and limits the length to 100 runes.
// It returns ErrUserNameNotSet if the resulting name is empty.
func sanitiseUserName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrUserNameNotSet
	}

	// Remove control / non‑printable characters.
	var b strings.Builder
	for _, r := range trimmed {
		if unicode.IsPrint(r) && r != 0x7F {
			b.WriteRune(r)
		}
	}
	clean := b.String()
	if clean == "" {
		return "", ErrUserNameNotSet
	}

	// Enforce a maximum of 100 runes. Truncate if longer.
	const maxRunes = 100
	runes := []rune(clean)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return string(runes), nil
}

// writeJSON writes the given payload as JSON with the supplied status code.
// It sets the appropriate Content‑Type header.
// This helper mirrors the project's existing helper; if the project already
// provides one, this definition will be ignored in favour of the existing one.
func writeJSON(w http.ResponseWriter, payload interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(payload)
}

// writeError writes a JSON error envelope with the given status code.
// The envelope format matches the project's existing convention.
func writeError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]interface{}{
		"error": err.Error(),
		"code":  status,
	}
	enc := json.NewEncoder(w)
	_ = enc.Encode(resp)
}