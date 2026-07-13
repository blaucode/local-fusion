1. **Create / update `auth/context.go`**
   - Add exported helper `func UserFromContext(ctx context.Context) (*User, error)`:
     - Use a fixed, unexported key (e.g., `userKey`).
     - Retrieve value; return `ErrUnauthenticated` if missing or type assertion fails.
     - Always return a **copy** of the user struct (field‑by‑field copy, not just a shallow pointer dereference) to avoid mutation races.
     - Do **not** insert a default name.
   - Expose `ErrUnauthenticated` and the `User` struct (ensure `Name` field is public).
   - Add a comment documenting that other packages must use this accessor.

2. **Create `handlers/greeting.go`**
   - Define `greetingResp struct { Message string `json:"message" }`.
   - Define a constant `greetingTemplate = "hello %s"` or a small builder function.
   - Implement `GreetingHandler(w http.ResponseWriter, r *http.Request)`:
     1. Check `r.Context().Err()`; if cancelled, return `408 Request Timeout` via `writeError`.
     2. Call `u, err := auth.UserFromContext(r.Context())`. On error:
        - If `errors.Is(err, auth.ErrUnauthenticated)`: set `Www-Authenticate` header if not present, call `writeError(w, err, http.StatusUnauthorized)`.
        - Else: `writeError(w, err, http.StatusInternalServerError)`.
     3. Validate `u.Name`:
        - Trim whitespace (spaces, tabs, newlines).
        - If empty after trim, return `400 Bad Request` with `writeError(w, errors.New("user name not set"), http.StatusBadRequest)`.
        - Strip ASCII control characters (0x00‑0x1F / 0x7F) and any other non-printable runes.
        - Truncate to 100 runes (or return `400` if longer, based on project’s preference – we’ll truncate with a comment).
     4. Format greeting string using `greetingTemplate` and sanitised name.
     5. Set security headers: `Cache‑Control: no-store, private`, `Vary: Authorization`, `X‑Content‑Type‑Options: nosniff`.
     6. `writeJSON(w, greetingResp{msg}, http.StatusOK)`.
     7. Log with `user_id` from `u.ID`.

3. **Register route in `handlers/router.go` (or equivalent)**
   - Locate the protected group (where auth middleware is applied).
   - Add `authRoute.Get("/greeting", GreetingHandler)` (or `HandleFunc` depending on router).
   - Ensure the route is **only** bound to GET (e.g., no `Handle` that accepts other verbs).
   - If the project uses a router that does not automatically reject other methods, add an explicit check in the handler or test it; otherwise rely on the router’s default 405 behavior.

4. **Extend / verify `writeJSON` and `writeError` helpers**
   - Confirm `writeError` produces a consistent JSON envelope (e.g., `{"error":"message","code":401}`). Adjust tests to match the exact shape.
   - If `writeError` does not set `Content-Type: application/json`, we add it.
   - If `writeJSON` does not already set the security headers, add a helper that both `writeJSON` and the greeting handler can use (or set them in `writeJSON` for all authenticated endpoints).

5. **Create `handlers/greeting_test.go`**
   - Use the real router (or the exported `authMiddleware` wrapper) to test the full chain.
   - Table-driven test with explicit fields:
     ```go
     struct {
         name        string
         token       string  // signed JWT with given claims (use test helper)
         httpMethod  string
         wantStatus  int
         wantBody    string // JSON string
         wantHeaders map[string]string
     }
     ```
   - **Minimum cases:**
     - No `Authorization` header → 401, `WWW-Authenticate` present.
     - Malformed JWT → 401.
     - Expired JWT → 401.
     - JWT signed with wrong key → 401.
     - Valid token, name `"Alice"` → 200, `{"message":"hello Alice"}`, headers present.
     - Valid token, name empty string → 400, error body.
     - Valid token, name whitespace only → 400.
     - Valid token, name with `<script>` → 200, message shows escaped/removed script (test JSON output ensures no injection).
     - Valid token, name with control characters → 200, message cleaned.
     - Valid token, name truncated to 100 chars (or returns 400 if policy).
     - `POST /greeting` → 405 Method Not Allowed (if router handles it; otherwise 404 or 401 depending on middleware).
   - **Concurrency test:** launch 10 goroutines that call the handler concurrently; verify no panics and all correct responses.
   - Validate the exact error envelope (e.g., `{"error":"unauthenticated","code":401}`).
   - Use a test helper to create signed tokens (ensure `testhelpers.SignedTokenForUser(name)` exists and supports empty claims).

6. **Create `handlers/greeting_bench_test.go`**
   - Benchmark a happy‑path request; check allocations and latency.

7. **Audit and fix middleware gaps (if any)**
   - Check that the auth middleware already sets `WWW-Authenticate` on 401; if not, add it there (or create a minimal wrapper for this endpoint’s error path).
   - Ensure the project’s global error handler does not strip the header.
   - Verify the rate‑limiting middleware is applied to the same route group; if not, add a `TODO` comment in the router registration.

8. **Update API documentation**
   - If `openapi.yaml` or `swagger.json` exists, add `GET /greeting` with response schemas and auth requirement.