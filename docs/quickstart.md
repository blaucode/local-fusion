# Quickstart

Host requirements: **docker + make**. Nothing else — no Go, no Python.

## 1. Configure keys

```sh
cp providers.env.example providers.env   # then paste your API keys into it
```

`providers.env` is gitignored. Details: [configuration](./configuration.md).

## 2. Build and run the server

```sh
make docker-build
make docker-run
```

The server listens on `http://localhost:8484` (bound to 127.0.0.1). Verify:

```sh
curl http://localhost:8484/healthz   # → ok
```

Your agent's skill does this same check before submitting work.

## 3. Connect your coding agent

Point the agent at `http://localhost:8484/mcp` — exact steps per agent in
[MCP setup](./mcp-setup.md).

## 4. Logs / stop

```sh
make docker-logs
make docker-stop
```

## Running without Docker networking (stdio)

Agents that must spawn a local process can use the kept stdio transport:

```sh
local-fusion serve --stdio
```

Note: under stdio the server lives and dies with your agent — in-flight jobs die with
it. HTTP is the primary transport for a reason; use stdio only when HTTP isn't an option.
