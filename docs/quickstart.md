# Quickstart

Get from zero to a connected agent in four steps. Host requirements: **docker + make**.

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

`make docker-run` requires `providers.env` to exist (step 1) and publishes the server on
`127.0.0.1:8484`. Verify it's up:

```sh
curl http://localhost:8484/healthz   # → ok
```

The skill runs this same check before it submits any work.

To score code (`lf_review`/`lf_judge`) or plan (`lf_plan`) the server also needs a model
registry — a v1-schema `providers.yaml` in the data volume. See
[configuration](./configuration.md#provider-registry).

## 3. Connect your coding agent

Point the agent at `http://localhost:8484/mcp` (Streamable HTTP) — exact steps per agent
in [MCP setup](./mcp-setup.md). Then install [the skill](./skill.md) so the agent knows
how to drive the loop.

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

Under stdio the server lives and dies with the agent process, so in-flight jobs die with
it. Prefer HTTP; use stdio only when your agent can't connect to a remote server.

## Next steps

- [Install the skill](./skill.md) — the agent-side guide that drives plan → build → judge.
- [Usage walkthrough](./usage.md) — run one feature end to end.
