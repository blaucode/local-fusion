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
*(Superseded for `lf_review`/`lf_judge` — see Amendment 2026-07-16; both are now jobs.)*
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

## Amendment (2026-07-16): review and judge move to async — the "1–2 calls" premise was wrong

The original decision kept `lf_review`, `lf_judge`, `lf_status` synchronous "because they're
1–2 model calls." The first live gated run (2026-07-16) disproved that for two of them:
`lf_review` is a **reviewer panel** (3 sequential models) and `lf_judge` is a **dual
reasoning-judge** panel where each call ran ~290s live — a round is ~10 minutes. Both
exceeded the in-app MCP client's tool timeout; a default Cline client (~60s) would fail
outright. The server always completed and wrote artifacts regardless of client disconnect —
which is exactly the property the submit→poll model exists to expose.

Decision: **`lf_review` and `lf_judge` become async jobs** (submit → `job_id`; poll
`lf_job`), stages `review` and `judge`, same idempotency/rediscovery mechanics as `lf_plan`.
`lf_status` stays synchronous (it makes no model calls). Consequences:

- The judge-retry ledger's escalate check (ADR-007) stays **synchronous at submit** — a third
  attempt returns `escalate_to_human` instantly with no job and no model calls, preserving the
  guarantee; the ledger is bumped inside the job on completion. So `lf_judge` returns *either*
  a `job_id` (submitted) *or* an immediate `escalate_to_human` (refused) — the agent handles
  both.
- Results (verdict/scores/coverage, review findings, and the `verdict.md`/`review.md` bytes)
  land in `job.result`, read via `lf_job`.
- No sync mode is kept — one consistent submit→poll surface; a fast-sync variant is additive
  later if a research profile ever wants it.

## Action Items
1. [x] Job runner with persistence + idempotent submit (M2, `55dd693`)
2. [x] Skill poll loop + stage-granular progress strings (skill/local-fusion/SKILL.md)
3. [x] Kill-switch test (budget_exhausted path) — runner tests + `make soak` (M2, `55dd693`)
