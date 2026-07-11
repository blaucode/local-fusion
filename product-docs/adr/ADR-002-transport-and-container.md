# ADR-002: Dual transport — Streamable HTTP (primary) + stdio (kept), containerized

**Status:** Accepted — S2 evidence green, Cline bar met (see Amendment 2026-07-11)
**Date:** 2026-07-09 · **Deciders:** Adolfo

## Context

v1 is stdio-only: one server process per agent, spawned by the agent, sharing its lifetime
and its host Python. This blocks containerization, multi-client use, and job persistence
across agent restarts (the async model in ADR-003 needs a server that outlives the client).
Some drivers (OpenHands) prefer HTTP natively.

## Decision

Serve MCP over **Streamable HTTP as the primary transport**, packaged as a Docker image
(distroless, `/data` volume, secrets via env). **Keep stdio working** (`serve --stdio`) —
same engine, two transports. Bind `127.0.0.1` by default; a **static bearer token**
(env var) is required whenever bound beyond localhost. No OAuth.

## Options Considered

### A: stdio only (status quo)
**Pros:** zero new surface; simplest. **Cons:** per-agent process spawn defeats the
long-lived job runner; container must be spawned by each agent; no shared server. Fails R1/R2.

### B: HTTP only
**Pros:** one transport to maintain. **Cons:** breaks local no-container use and v1-style
configs during migration; needless adoption cliff.

### C: HTTP primary + stdio kept *(chosen)*
**Pros:** container + multi-client + job persistence; graceful migration; stdio is a thin
shim over the same tool layer. **Cons:** two transports to test (mitigated: contract tests
run against both).

## Trade-off Analysis

The async job model is the product's core fix; it requires a server with its own lifetime →
HTTP. stdio costs little to keep (the MCP SDK provides both) and buys back-compat plus a
fallback if S2 finds client bugs in Streamable HTTP (Cline is the historical problem child).

## Amendments (2026-07-09, external review)

**Provider-capacity policy (account-level contention).** The unit/slot pools model one
server's view, but provider limits are per *account*. Policy: **shared key ⇒ shared server**
(that server is the sole arbiter — global semaphore per provider account); **per-dev servers
⇒ per-dev keys**. The unsupported configuration — multiple servers sharing one account key —
is explicitly forbidden in setup docs; it silently breaks fairness and the concurrency math.
Pilot exposure is low (solo-coder profile uses few units), but the rule is stated now.

**stdio fallback is a partial retreat, not an equivalent.** If the S2 spike fails and we
launch stdio-first, the async model keeps its *timeout* fix (submit returns instantly, polls
are short) but **loses crash-resilience**: a stdio server dies with its agent, killing
in-flight jobs. Persistent job state allows idempotent resubmit that skips completed steps,
but that is new design work, not a free property. Any stdio-first decision must say this
out loud and re-raise HTTP as the target, not the fallback-forever.

## Amendment (2026-07-11): S2 spike evidence — SDK + Streamable HTTP work; Cline/Cursor pending

Measured (spike code: `spikes/s2-echo`, official `modelcontextprotocol/go-sdk` **v1.6.1**,
server in a `golang:1.25` container on `127.0.0.1:8484`):

- **Toolchain finding:** SDK v1.6.1 requires **Go ≥ 1.25**; adopting it means bumping the
  repo's pinned `golang:1.23` image to `golang:1.25` (spikes already run on 1.25).
- **Protocol (raw probe):** initialize → SSE + `Mcp-Session-Id`; `notifications/initialized`
  202; `tools/list` returns `lf_echo` with input **and** output schemas auto-inferred from
  Go structs (`jsonschema` tags); `tools/call` returns `content` + `structuredContent`.
- **Claude Code by URL: PASS.** `claude mcp add --transport http … http://localhost:8484/mcp`
  → connected; a `claude -p` session discovered and called `lf_echo`, returning the exact
  structured JSON.
- **Cline (the acceptance bar): PASS** — owner-verified in GUI same day. Cline connected
  by URL (`"type": "streamableHttp"` in `cline_mcp_settings.json`) and called `lf_echo`:
  `{"echo":"hello from cline","server":"lf-spike-s2"}`. The historical problem child
  works; **S2 verdict: PASS — Streamable HTTP confirmed as primary transport**, the
  stdio-first partial retreat is not invoked (stdio still ships as the kept secondary).
- **Cursor: untested** (non-bar; config staged in `~/.cursor/mcp.json`). Record the
  result here when it happens; a Cursor failure would be a client bug to track, not a
  transport-decision change.

## Consequences

- Easier: `docker run` adoption, shared team server later (per-dev first, Q11), agent
  crash-resilience.
- Harder: auth story exists at all (token); health/liveness endpoint needed; version skew
  between one server and many skills — skill declares min server version.
- Revisit if: S2 shows Streamable HTTP unusable on any target agent → stdio-first launch,
  HTTP behind a flag.

## Action Items
1. [x] S2 spike: echo over Streamable HTTP — Claude Code PASS, Cline PASS (bar met), Cursor untested/non-bar (Amendment 2026-07-11)
2. [ ] `GET /healthz`; skill checks before first submit
3. [ ] Token middleware + refuse non-localhost bind without token
