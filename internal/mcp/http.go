package mcp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// HTTPConfig configures the Streamable HTTP transport (ADR-002).
type HTTPConfig struct {
	// Addr is the listen address. Default "127.0.0.1:8484".
	Addr string
	// Token is the static bearer token (from env, never argv). When set, every
	// /mcp request must carry it. It is REQUIRED when Addr binds beyond
	// localhost (ADR-002: refuse non-localhost bind without token).
	Token string
	// InsecureNoToken permits a non-localhost bind without a token. Exists for
	// exactly one deployment: inside the container, where the process must bind
	// 0.0.0.0 and the loopback-only guarantee is docker's published port
	// (`-p 127.0.0.1:8484:8484`, the make docker-run default).
	InsecureNoToken bool
}

// ValidateBind enforces the ADR-002 rule: binding beyond localhost without a
// bearer token is refused at startup, not discovered in production.
// insecureNoToken is the explicit, operator-visible override.
func ValidateBind(addr, token string, insecureNoToken bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	if token != "" || insecureNoToken {
		return nil
	}
	switch {
	case host == "localhost":
		return nil
	case host != "":
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return errors.New("refusing to bind beyond localhost without LF_AUTH_TOKEN — see docs/configuration.md#auth")
}

// Handler assembles the HTTP surface: /mcp (token-guarded when configured)
// and GET /healthz (always open, for the skill's pre-submit check).
func Handler(server *sdk.Server, cfg HTTPConfig) http.Handler {
	streamable := sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return server }, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", requireToken(cfg.Token, streamable))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

func requireToken(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ServeHTTP runs the HTTP transport until ctx is cancelled, then shuts down
// gracefully (in-flight MCP requests get a drain window).
func ServeHTTP(ctx context.Context, server *sdk.Server, cfg HTTPConfig) error {
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8484"
	}
	if err := ValidateBind(cfg.Addr, cfg.Token, cfg.InsecureNoToken); err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           Handler(server, cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("mcp http listening", "addr", cfg.Addr, "auth", cfg.Token != "")
		err := httpServer.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

// ServeStdio runs the kept-secondary stdio transport (ADR-002) until the
// client disconnects or ctx is cancelled.
func ServeStdio(ctx context.Context, server *sdk.Server) error {
	return server.Run(ctx, &sdk.StdioTransport{})
}
