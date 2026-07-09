# ADR-002: Dual transport — Streamable HTTP (primary) + stdio (kept), containerized

**Status:** Accepted (pending M1 S2 evidence)
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

## Consequences

- Easier: `docker run` adoption, shared team server later (per-dev first, Q11), agent
  crash-resilience.
- Harder: auth story exists at all (token); health/liveness endpoint needed; version skew
  between one server and many skills — skill declares min server version.
- Revisit if: S2 shows Streamable HTTP unusable on any target agent → stdio-first launch,
  HTTP behind a flag.

## Action Items
1. [ ] S2 spike: echo tool over Streamable HTTP against all 3 agents (Cline = bar)
2. [ ] `GET /healthz`; skill checks before first submit
3. [ ] Token middleware + refuse non-localhost bind without token
