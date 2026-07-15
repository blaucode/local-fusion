# ADR-007: Engine-enforced budgets and termination

**Status:** Accepted
**Date:** 2026-07-09 · **Deciders:** Adolfo

## Context

v1's termination rules are skill-prompt conventions ("re-judge once, then stop") — a
well-behaved agent follows them; a confused one loops. Loop-engineering canon: termination
is half the design — layered exits (verifier, step cap, time/token budget, no-progress
detection), enforced by the system, not requested of the model.

## Decision

The job runner enforces, per job, with config defaults and per-call overrides:
**wall-clock budget** (plan 30m/task default, coder-fusion 15m), **step cap**
(max model calls), **token budget** (bounds latency even on flat-rate plans),
**no-progress detection** (same stage failing twice identically → job fails, no silent
degradation loop), and a **judge-retry ledger** in the manifest — a third judge attempt on
the same task returns `escalate_to_human`, turning v1's convention into a guarantee.
Error taxonomy: `recoverable` (model dropout → degrade, v1 semantics kept) vs `fatal`
(missing key, budget, no-progress) — always surfaced in job status, never a hang.

## Options Considered

### A: Keep conventions in the skill — works until it doesn't; unattended/overnight use
(S2 scenario) is irresponsible without hard stops. Rejected as sole mechanism (skill keeps
the conventions as *narration*, engine holds the *law*).
### B: Global budgets only (server-wide caps) — blunt; one runaway plan starves other jobs
invisibly. Rejected.
### C: Per-job layered budgets + ledger *(chosen)*.

## Trade-off Analysis

This is cheap insurance (a context deadline, counters, and a small ledger) against the
expensive failure class — silent token burn and stuck loops — that kills trust in unattended
operation. PRD Goal 3 and the kill-switch success criterion depend on it.

## Amendment (2026-07-11): S3 spike evidence — kill-switch mechanism PASS

Measured (spike code: `spikes/s3-killswitch`, `go test -race` in `golang:1.25` container):
a 150ms `context.WithTimeout` budget cut through an errgroup panel of three fake
10-minute wedged stages in exactly 0.15s (`context.DeadlineExceeded` surfaced, never a
hang); a 20-job cancellation storm (100ms budgets) drained every panel goroutine — no
leaks (`runtime.NumGoroutine` settles), no races. The context-deadline + errgroup design
this ADR mandates for `internal/jobs` is confirmed workable; M2 implements it against
real provider calls (cancellability of in-flight HTTP is Q4's residual risk, covered by
the M2 kill-switch exit-gate test).

## Consequences

- Easier: overnight jobs, cost predictability, debugging (budget_exhausted says *where*).
- Harder: defaults need tuning against real stage timings (start from v1's measured ~7
  min/task plan, 420s judge calls); partial artifacts on kill must be coherent (write-as-you-go,
  already v1's style).
- Revisit: per-stage token *input* budgets once Q8 metrics exist (32K ceiling).

## Action Items
1. [x] Budgets in `internal/jobs` via context deadlines + call counters (M2, `55dd693`)
2. [x] Judge-retry ledger in manifest; `escalate_to_human` refusal in `lf_judge` (P1)
3. [x] Kill-switch test = runner tests + `make soak` (shared with ADR-003)
