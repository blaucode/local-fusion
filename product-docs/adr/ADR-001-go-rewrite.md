# ADR-001: Rewrite the engine in Go

**Status:** Accepted (pending M1 spike evidence on Q1/Q3 — see Action Items)
**Date:** 2026-07-09 · **Deciders:** Adolfo

## Context

v1 is Python 3.14 + FastMCP + curl subprocesses. It works, but distribution is the pain:
interpreter mismatches (documented 90%-of-failures cause in v1 setup docs), PEP 668 pip
friction, and a curl dependency because Cloudflare blocked `urllib`. v2's goals shift from
"validate the pipeline" to "a teammate runs this in 15 minutes" — distribution, concurrency
(weighted provider pools), and containerization dominate.

## Decision

Rewrite the server/engine in **Go** as a single static binary. The **pipeline logic —
prompts, config schema, artifact formats — is the contract to preserve**, not the language
(prompts become embedded data, ADR-008; schema unchanged).

## Options Considered

### A: Stay Python (harden packaging: uv/pyinstaller/docker)
Complexity Low · Cost Low · Distribution Medium · Concurrency Medium (asyncio refactor needed anyway)
**Pros:** zero port risk; validated code untouched; mature MCP SDK.
**Cons:** interpreter/deps pain only masked, not removed; the async job runner + weighted
semaphore pools mean a large refactor of v1 regardless — the "no rewrite" option still
rewrites the hard part.

### B: Go single binary *(chosen)*
Complexity Medium · Cost Medium (port effort) · Distribution Excellent · Concurrency Native
**Pros:** one artifact, trivial container (distroless), goroutines/errgroup map directly to
panel fan-out and unit/slot pools; no runtime deps for teammates.
**Cons:** port risk (mitigated by parity gates + Python proxy in M2/M3); Go MCP SDK younger
than Python's (spike S2); TLS fingerprinting unknown (spike S1).

### C: Hybrid forever (Go shell, Python engine as subprocess)
**Pros:** M2 exists anyway; minimal port. **Cons:** two runtimes to ship = the distribution
problem unsolved; permanent IPC seam. Rejected as end-state, embraced as **migration stage**.

## Trade-off Analysis

The deciding constraint is Goal 2 (15-minute adoption on machines we don't control). Only B
achieves it fully. Port risk is bounded because B is reached *through* C with per-stage
parity gates — we never bet the working system on the rewrite.

## Consequences

- Easier: distribution, container story, provider concurrency, teammate onboarding.
- Harder: two codebases during M2–M3; parity testing burden; any v1 hotfix must land twice.
- Revisit if: M1 spikes fail without acceptable fallback, or M3 parity proves unreachable.

## Action Items
1. [ ] Spike S1 (Q1): `net/http` vs Featherless/Cloudflare — amend this ADR with result
2. [ ] Spike S2 (Q3): Go MCP SDK vs Claude Code/Cline/Cursor — amend ADR-002 with result
3. [ ] M0: prompt extraction (ADR-008) before any Go engine code
