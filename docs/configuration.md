# Configuration

All configuration arrives via environment variables (or `providers.env`, passed with
`--env-file` — see the [quickstart](./quickstart.md)) and two `serve` flags. Secrets are
env-only by design: command-line arguments are visible in `ps`.

## Provider keys

| Variable | Provider | Where to get it |
|---|---|---|
| `FEATHERLESS_API_KEY` | Featherless | https://featherless.ai → API Keys |
| `OLLAMA_API_KEY` | Ollama Cloud | https://ollama.com → settings → API Keys |

Copy `providers.env.example` to `providers.env` and fill these in. Never commit the real
file (it's gitignored); the server never logs key material.

## Providers

`lf_review` and `lf_judge` need the model registry: a **v1-schema `providers.yaml`**
(providers, models, pipelines — your existing v1 file works unmodified). Put it at
`/data/providers.yaml` (inside the volume) or point `serve --config` at it. Without it
the server still runs; the stage tools answer with a structured error pointing here.

`LF_USER` (optional) attributes `metrics.jsonl` records to you; defaults to `$USER`.

## Project constitution (optional)

Place a `constitution.md` in the data volume at `projects/<project_id>/constitution.md` to
give a project persistent, human-authored principles — the non-negotiables a plan must
honor and a judge scores against ("use the repo's auth middleware; never inline SQL; tests
required for every endpoint"). When present it is injected (append-only) into the plan
synthesizer and `lf_judge`, so both the planning brain and the gate measure against it.
Absent = today's behavior. Keep it small and human-edited (about one screen). `lf_status`
reports `constitution_active`. (ADR-012.)

## Auth

`LF_AUTH_TOKEN` — static bearer token protecting `/mcp`.

- **Localhost (default):** optional. `serve` binds `127.0.0.1:8484`; if the token is
  unset, local clients connect without auth.
- **Beyond localhost:** required. The server **refuses to start** on a non-loopback
  address without `LF_AUTH_TOKEN` set. Clients then send
  `Authorization: Bearer <token>`.
- `GET /healthz` is always unauthenticated (liveness only, returns `ok`).
- **Inside the container** the process binds `0.0.0.0` with the explicit
  `--insecure-no-token` override (baked into the image CMD) — there, the loopback-only
  guarantee is docker's published port: `make docker-run` publishes `127.0.0.1:8484`.
  If you publish the port beyond localhost (`-p 8484:8484` on a shared host), set
  `LF_AUTH_TOKEN` in your env-file; the token check is enforced whenever the token is set.

## `serve` flags

| Flag | Default | Meaning |
|---|---|---|
| `--addr` | `127.0.0.1:8484` | HTTP listen address (`host:port`) |
| `--stdio` | off | serve MCP over stdio instead of HTTP |

## Logs

Structured JSON on stderr. Per-job/stage/provider fields arrive with the job runner
(M2); the log shape (JSON lines) is stable from the first release.
