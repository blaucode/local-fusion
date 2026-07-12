// Command local-fusion is the v2 server binary: a multi-model quality gate for
// coding agents, exposed over MCP (Streamable HTTP primary, stdio kept — ADR-002).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"local-fusion/internal/engine/providers"
	"local-fusion/internal/jobs"
	"local-fusion/internal/mcp"
	"local-fusion/internal/store"
	"local-fusion/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(version.String())
	case "serve":
		if err := serve(os.Args[2:]); err != nil {
			slog.Error("serve failed", "err", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8484", "HTTP listen address (host:port)")
	stdio := fs.Bool("stdio", false, "serve MCP over stdio instead of HTTP (kept transport, ADR-002)")
	insecure := fs.Bool("insecure-no-token", false, "allow non-localhost bind without LF_AUTH_TOKEN (container-internal use only — see docs/configuration.md#auth)")
	dataDir := fs.String("data", "/data", "artifact volume root (ADR-005)")
	workers := fs.Int("workers", 4, "max concurrently running jobs")
	configPath := fs.String("config", "/data/providers.yaml", "providers.yaml (v1 schema) — required for lf_review/lf_judge")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// JSON logs from day one so the shape never changes on operators. Logs go
	// to stderr in both modes — in stdio mode stdout belongs to the transport.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.New(*dataDir)
	if err != nil {
		return err
	}
	runner := jobs.NewRunner(*workers, st, slog.Default())
	defer runner.Close()

	// providers.yaml is optional at startup: without it the stage tools
	// answer with a structured pointer to the docs instead of vanishing.
	var cfg *providers.Config
	if loaded, err := providers.Load(*configPath); err == nil {
		cfg = loaded
		slog.Info("providers config loaded", "path", *configPath, "models", len(cfg.Models))
	} else {
		slog.Warn("providers config not loaded — lf_review/lf_judge disabled", "path", *configPath, "err", err)
	}
	user := os.Getenv("LF_USER")
	if user == "" {
		user = os.Getenv("USER")
	}

	server := mcp.NewServer()
	mcp.RegisterTools(server, mcp.Deps{Runner: runner, Store: st})
	mcp.RegisterStageTools(server, mcp.EngineDeps{
		Store: st, Cfg: cfg,
		Caller: &providers.Client{Env: os.Getenv, Log: func(m string) { slog.Info(m) }},
		Log:    func(m string) { slog.Info(m) },
		User:   user, Ver: version.Version,
	})

	if *stdio {
		slog.Info("mcp stdio serving", "version", version.String())
		return mcp.ServeStdio(ctx, server)
	}
	return mcp.ServeHTTP(ctx, server, mcp.HTTPConfig{
		Addr: *addr,
		// Token comes from env only — argv shows up in `ps`, env does not.
		Token:           os.Getenv("LF_AUTH_TOKEN"),
		InsecureNoToken: *insecure,
	})
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: local-fusion <command>

commands:
  serve [--addr 127.0.0.1:8484] [--stdio] [--data /data] [--workers 4]
        [--config /data/providers.yaml]      run the MCP server
  version                                    print build version`)
}
