# ADR-003: Async job model for long stages

**Status:** Accepted
**Date:** 2026-07-09 · **Deciders:** Adolfo

## Context

`lf_plan` ≈ 7 sequential model calls / ~7 min **per task**; multi-task plans exceed an hour —
past even v1's `timeout: 3600` stopgap. Cline's default MCP timeout is ~60s; OpenCode's
timeout doesn't even cover per-tool duration. This is v1's single biggest limitation
(documented as such in v1 `documentation/12`), and every "flaky driver" symptom traces to it.

## Decision

Long stages (`lf_plan`, `lf_coder_fusion`) become **jobs**: submit returns a `job_id` in
<2s; `lf_job(job_id)` polls `{status, progress, partial, result?, error?}`; `lf_cancel`
is cooperative. Jobs and results **persist in the store** — a crashed agent reconnects and
polls. Short stages (`lf_review`, `lf_judge`, `lf_status`) stay synchronous (1–2 calls).
Lifecycle: `queued → running → done | failed | cancelled | budget_exhausted`.

## Options Considered

### A: Keep sync + document big timeouts (v1 stopgap)
**Pros:** nothing to build. **Cons:** already failing (multi-task > 3600s; OpenCode can't be
configured around it); couples job lifetime to client lifetime. Rejected — this is the problem.

### B: MCP progress streaming/notifications on a long-lived call
**Pros:** no polling. **Cons:** still one long-lived request — client timeout and disconnect
kill the job; uneven client support. Rejected as primary (may *supplement* polling later).

### C: Submit → poll *(chosen)*
**Pros:** immune to client timeouts/disconnects by construction; trivially testable; skill
change is ~10 lines (poll every 30–60s). **Cons:** polling latency (irrelevant at
minutes-scale); job state to persist (needed anyway for budgets/ledger).

## Trade-off Analysis

At minutes-to-hour durations, robustness against client lifecycle beats elegance. Polling is
the boring, proven pattern; every alternative re-couples job survival to connection survival.

## Consequences

- Easier: default agent configs work; overnight planning (S2 scenario); budget enforcement
  has a natural home (the job runner).
- Harder: skill must poll and narrate; results outlive requests → store required (ADR-005's
  volume covers it); idempotent submit needed (same slug+stage while running → return
  existing job, don't double-run).
- Revisit: streaming progress as a P1 UX add-on (R10) once polling is solid.

## Amendment (2026-07-09, external review): reconnect & idempotency mechanics

- **Job identity:** `job_id` is derived-stable — keyed `(project_id, slug, stage, task_id)`.
  Submitting while an identical job is `queued|running` returns the existing `job_id`
  (idempotent submit); no double-run.
- **Rediscovery:** a crashed/restarted agent calls `lf_status(project_id, slug)`, which lists
  active and recent jobs with their `job_id`s — nothing needs to survive in agent memory.
- **Same-key, different-context resubmit:** rejected with `conflict` while the original runs
  (`lf_cancel` first); allowed after terminal state, recorded as a new attempt in the manifest.
- **Crash-resilience caveat:** these guarantees assume the HTTP deployment (server outlives
  client). Under the stdio fallback they degrade — see ADR-002 amendment.

## Action Items
1. [x] Job runner with persistence + idempotent submit (M2, `55dd693`)
2. [ ] Skill poll loop + stage-granular progress strings
3. [x] Kill-switch test (budget_exhausted path) — runner tests + `make soak` (M2, `55dd693`)
